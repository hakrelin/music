// Package httpapi 提供 HTTP 路由、WS 升级入口与前端静态资源。
//
// 路由划分（docs §3）：
//   - /api/** 全是 JSON（WS 除外），错误统一 {"error":{"code","message"}}；
//   - 其余路径是 SPA：命中真实文件就发文件，否则 fallback 到 index.html。
package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/hakrelin/music/server/internal/config"
	"github.com/hakrelin/music/server/internal/hub"
	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider"
	"github.com/hakrelin/music/server/internal/provider/demo"
	"github.com/hakrelin/music/server/internal/webui"
)

// Server 聚合 HTTP 层所需的依赖。
type Server struct {
	cfg     config.Config
	hub     *hub.Hub
	reg     *provider.Registry
	demo    *demo.Provider // 仅 demo provider 启用时非 nil
	version string
	log     *slog.Logger
	static  fs.FS
}

// Options 构造 Server。
type Options struct {
	Config   config.Config
	Hub      *hub.Hub
	Registry *provider.Registry
	Demo     *demo.Provider
	Version  string
	Logger   *slog.Logger
}

// New 构造 HTTP 服务。
func New(opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		cfg:     opts.Config,
		hub:     opts.Hub,
		reg:     opts.Registry,
		demo:    opts.Demo,
		version: opts.Version,
		log:     logger,
		static:  webui.FS(),
	}
}

// Handler 返回完整路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/rooms", s.handleRooms)
	mux.HandleFunc("POST /api/rooms", s.handleCreateRoom)
	mux.HandleFunc("POST /api/rooms/{id}/auth", s.handleRoomAuth)
	mux.HandleFunc("GET /api/ws", s.hub.ServeWS)

	// demo provider 的音频/封面：即使没注册 demo 也挂上路由，内部返回 404，
	// 这样部署切到 wy 后旧的 demo 封面 URL 不会变成 HTML fallback。
	mux.HandleFunc("GET /api/demo/audio/{file}", s.handleDemoAudio)
	mux.HandleFunc("GET /api/demo/cover/{file}", s.handleDemoCover)

	// 未匹配的 /api 路径统一 JSON 404（不能被 SPA fallback 吃掉）。
	mux.HandleFunc("/api", s.handleAPINotFound)
	mux.HandleFunc("/api/", s.handleAPINotFound)

	// 其余都是静态资源 + SPA fallback。
	mux.HandleFunc("/", s.handleStatic)

	return withCommonHeaders(mux)
}

// withCommonHeaders 给所有响应加上安全/编码相关的头。
func withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// ---------- API ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"version":  s.version,
		"provider": s.hub.PrimaryProvider(),
		"rooms":    s.hub.RoomCount(),
	})
}

type siteOut struct {
	Name    string `json:"name"`
	Tagline string `json:"tagline"`
}

type sourceOut struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type configOut struct {
	Site         siteOut      `json:"site"`
	Sources      []sourceOut  `json:"sources"`
	RadioPresets []hub.Preset `json:"radioPresets"`
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, configOut{
		Site:         siteOut{Name: s.cfg.Site.Name, Tagline: s.cfg.Site.Tagline},
		Sources:      s.sources(),
		RadioPresets: s.hub.Presets(),
	})
}

// sources 列出可用音乐源。wy 排前面（真实源优先），demo 始终兜底。
func (s *Server) sources() []sourceOut {
	labels := map[string]string{
		"wy":   "网易云音乐",
		"demo": "演示电台",
	}
	rank := func(id string) int {
		switch id {
		case "wy":
			return 0
		case "demo":
			return 1
		default:
			return 2
		}
	}
	names := s.reg.Names()
	sort.Slice(names, func(i, j int) bool {
		if rank(names[i]) != rank(names[j]) {
			return rank(names[i]) < rank(names[j])
		}
		return names[i] < names[j]
	})
	out := make([]sourceOut, 0, len(names))
	for _, n := range names {
		label, ok := labels[n]
		if !ok {
			label = n
		}
		out = append(out, sourceOut{ID: n, Label: label})
	}
	return out
}

func (s *Server) handleRooms(w http.ResponseWriter, _ *http.Request) {
	rooms := s.hub.Rooms()
	if rooms == nil {
		rooms = []model.RoomSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

type createRoomReq struct {
	OwnerPassword string `json:"ownerPassword"`
	Name          string `json:"name"`
	Desc          string `json:"desc"`
	NeedPwd       bool   `json:"needPwd"`
	Password      string `json:"password"`
}

func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	var req createRoomReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	room, err := s.hub.CreateRoom(req.Name, req.Desc, req.NeedPwd, req.Password, req.OwnerPassword)
	if err != nil {
		var ve *hub.ValidationError
		if asValidation(err, &ve) {
			writeError(w, http.StatusBadRequest, "bad_request", ve.Message)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "创建房间失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": room.ID(), "ownerPassword": room.OwnerPassword()})
}

func (s *Server) handleRoomAuth(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	room, ok := s.hub.Room(id)
	if !ok {
		// 与"密码错"统一，避免枚举房间（docs §3）。
		writeError(w, http.StatusForbidden, "bad_password", "房间不存在或口令不对")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		// 空 body 视为空口令，仍然走校验。
		req.Password = ""
	}
	if !room.CheckPassword(req.Password) {
		writeError(w, http.StatusForbidden, "bad_password", "房间不存在或口令不对")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "接口不存在："+r.URL.Path)
}

// decodeJSON 读取并解码 JSON body，限制 64KB 防止恶意大包。
// 不启用 DisallowUnknownFields：契约之外的附加字段应当被忽略，
// 这样前端/调用方加字段不会把老服务端打挂。
func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	return dec.Decode(dst)
}

// asValidation 是 errors.As 的小包装，避免在调用处重复写类型断言。
func asValidation(err error, target **hub.ValidationError) bool {
	ve, ok := err.(*hub.ValidationError)
	if ok {
		*target = ve
	}
	return ok
}

// ---------- demo 音频 / 封面 ----------

func (s *Server) handleDemoAudio(w http.ResponseWriter, r *http.Request) {
	if s.demo == nil {
		writeError(w, http.StatusNotFound, "demo_disabled", "演示音乐源未启用")
		return
	}
	file := r.PathValue("file")
	id := strings.TrimSuffix(file, ".wav")
	if id == file || id == "" || strings.ContainsAny(id, "/\\") {
		writeError(w, http.StatusNotFound, "not_found", "演示音频不存在")
		return
	}
	data, ok := s.demo.Audio(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "演示音频不存在")
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	// ServeContent 负责 Range / HEAD / Content-Length，音频元素 seek 需要它。
	http.ServeContent(w, r, file, time.Time{}, bytes.NewReader(data))
}

func (s *Server) handleDemoCover(w http.ResponseWriter, r *http.Request) {
	if s.demo == nil {
		writeError(w, http.StatusNotFound, "demo_disabled", "演示音乐源未启用")
		return
	}
	file := r.PathValue("file")
	id := strings.TrimSuffix(file, ".svg")
	if id == file || id == "" || strings.ContainsAny(id, "/\\") {
		writeError(w, http.StatusNotFound, "not_found", "演示封面不存在")
		return
	}
	data, ok := s.demo.Cover(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "演示封面不存在")
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ---------- 静态资源 / SPA ----------

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		s.serveIndex(w, r)
		return
	}
	// 先尝试真实文件（assets/xxx.js、favicon.svg……）。
	if data, err := fs.ReadFile(s.static, name); err == nil {
		s.serveFile(w, r, name, data)
		return
	}
	// 未知路径交给 SPA 路由（vue-router history 模式）。
	s.serveIndex(w, r)
}

// serveFile 按路径决定缓存策略：
//   - /assets/ 下是 Vite 带 hash 的产物，长缓存即可；
//   - index.html 必须 no-cache，否则发版后用户拿到旧壳；
//   - 其余短缓存。
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = http.DetectContentType(data)
	}
	if strings.HasPrefix(ctype, "text/") || strings.HasSuffix(ctype, "javascript") ||
		strings.HasSuffix(ctype, "json") || strings.HasSuffix(ctype, "xml") {
		if !strings.Contains(ctype, "charset") {
			ctype += "; charset=utf-8"
		}
	}
	w.Header().Set("Content-Type", ctype)
	switch {
	case strings.HasPrefix(name, "assets/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case name == "index.html":
		w.Header().Set("Cache-Control", "no-cache")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

// serveIndex 返回 SPA 外壳；前端还没构建时给一条可读的提示。
func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.static, "index.html")
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusNotFound)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte("前端尚未构建：请先运行 npm --prefix web run build && node scripts/copy-dist.mjs\n" +
				"（API 仍然可用：/api/healthz、/api/rooms、/api/ws）\n"))
		}
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}
