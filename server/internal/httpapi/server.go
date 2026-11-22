// Package httpapi 提供 HTTP 路由、WS 升级入口、管理后台 API 与前端静态资源。
//
// 路由划分（docs §3）：
//   - /api/** 全是 JSON（WS 与音频流除外），错误统一 {"error":{"code","message"}}；
//   - 其余路径是 SPA：命中真实文件就发文件，否则 fallback 到 index.html。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hakrelin/music/server/internal/config"
	"github.com/hakrelin/music/server/internal/hub"
	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider"
	"github.com/hakrelin/music/server/internal/provider/bilibili"
	"github.com/hakrelin/music/server/internal/provider/demo"
	"github.com/hakrelin/music/server/internal/provider/netease"
	"github.com/hakrelin/music/server/internal/storage"
	"github.com/hakrelin/music/server/internal/webui"
)

// Server 聚合 HTTP 层所需的依赖。
type Server struct {
	cfgMu        sync.RWMutex
	cfg          config.Config
	meta         config.Meta
	hub          *hub.Hub
	reg          *provider.Registry
	demo         *demo.Provider // 仅 demo provider 启用时非 nil
	netease      *netease.Provider
	bilibili     *bilibili.Provider
	cache        *storage.Holder
	cacheResolve storage.Resolver
	rebuildCache func(config.Config) *storage.Cache
	version      string
	log          *slog.Logger
	static       fs.FS

	adminMu       sync.Mutex
	adminSessions map[string]time.Time
	loginFails    map[string][]time.Time
}

// Options 构造 Server。
type Options struct {
	Config       config.Config
	Meta         config.Meta
	Hub          *hub.Hub
	Registry     *provider.Registry
	Demo         *demo.Provider
	Netease      *netease.Provider
	Bilibili     *bilibili.Provider
	Cache        *storage.Holder
	CacheResolve storage.Resolver
	RebuildCache func(config.Config) *storage.Cache
	Version      string
	Logger       *slog.Logger
}

// New 构造 HTTP 服务。
func New(opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	meta := opts.Meta
	if meta.SecretsPath == "" {
		meta.SecretsPath = config.ResolveSecretsPath(opts.Config)
	}
	if meta.Secrets.Version <= 0 {
		meta.Secrets.Version = 1
	}
	wy := opts.Netease
	if wy == nil {
		wy = netease.New(netease.Config{Cookie: opts.Config.NeteaseCookie})
	}
	bili := opts.Bilibili
	if bili == nil {
		bili = bilibili.New(bilibili.Config{
			Cookie:   opts.Config.Bilibili.Cookie,
			UseLogin: opts.Config.Bilibili.UseLogin,
		})
	}
	return &Server{
		cfg:           opts.Config.Clone(),
		meta:          meta,
		hub:           opts.Hub,
		reg:           opts.Registry,
		demo:          opts.Demo,
		netease:       wy,
		bilibili:      bili,
		cache:         opts.Cache,
		cacheResolve:  opts.CacheResolve,
		rebuildCache:  opts.RebuildCache,
		version:       opts.Version,
		log:           logger,
		static:        webui.FS(),
		adminSessions: make(map[string]time.Time),
		loginFails:    make(map[string][]time.Time),
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

	// 音频回退 / 对象存储缓存 / B 站纯音频透传代理。
	mux.HandleFunc("GET /api/stream/{id}", s.handleStream)
	mux.HandleFunc("HEAD /api/stream/{id}", s.handleStream)
	mux.HandleFunc("GET /api/cache/stats", s.handleCacheStats)

	// 管理后台接口（root 口令登录 + Cookie 认证）。
	s.registerAdminRoutes(mux)

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
	s.cfgMu.RLock()
	site := siteOut{Name: s.cfg.Site.Name, Tagline: s.cfg.Site.Tagline}
	s.cfgMu.RUnlock()
	writeJSON(w, http.StatusOK, configOut{
		Site:         site,
		Sources:      s.sources(),
		RadioPresets: s.hub.Presets(),
	})
}

// sources 列出前端点歌面板可选的音乐源。
// 主音源排第一位（wy 生效时隐藏 demo，保持界面简洁；demo 模式下 demo 排第一、bili 排第二）。
func (s *Server) sources() []sourceOut {
	labels := map[string]string{
		"wy":   "网易云音乐",
		"bili": "哔哩哔哩",
		"demo": "演示电台",
	}
	primary := s.hub.PrimaryProvider()
	rank := func(id string) int {
		if id == primary {
			return 0
		}
		switch id {
		case "wy":
			return 1
		case "bili":
			return 2
		case "demo":
			return 3
		default:
			return 4
		}
	}
	raw := s.reg.Names()
	names := make([]string, 0, len(raw))
	for _, n := range raw {
		if n == "demo" && primary != "demo" {
			continue
		}
		names = append(names, n)
	}
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

// ---------- 音频回退 / 对象存储缓存 / B站纯音频代理 ----------

// handleStream 是播放器的缓存与代理入口：
//   - 命中对象存储：302 到 COS/CDN 地址（浏览器直连，不吃服务器带宽）；
//   - B 站等强校验 Referer 的音源：未命中时代理透传纯音频流（带 Referer + Range），并后台预热对象存储；
//   - 网易云未命中：现场解析并 302 到健康 CDN 节点，同时后台预热缓存。
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	id = strings.TrimSuffix(id, ".mp3")
	id = strings.TrimSuffix(id, ".m4a")
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if source == "" {
		source = s.hub.PrimaryProvider()
	}
	if id == "" || len(id) > 64 || strings.ContainsAny(id, "/\\?#") || strings.Contains(id, "..") {
		writeError(w, http.StatusBadRequest, "bad_request", "曲目 ID 不合法")
		return
	}
	// 302 的目标随时会变（预签名/签名都有有效期），禁止中间层缓存这次跳转。
	w.Header().Set("Cache-Control", "no-store")

	if s.cache != nil {
		if u, ok := s.cache.Lookup(source, id); ok {
			w.Header().Set("X-Cache", "hit")
			http.Redirect(w, r, u, http.StatusFound)
			return
		}
	}

	p, ok := s.reg.For(source)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_source", "未知音乐源："+source)
		return
	}
	if sp, ok := p.(provider.StreamProxyProvider); ok {
		lower := r.URL.Query().Get("lower") == "1" || strings.EqualFold(r.URL.Query().Get("lower"), "true")
		if r.Method == http.MethodGet && s.cache != nil && s.cacheResolve != nil && s.cache.Enabled() {
			s.cache.Ensure(source, id, s.cacheResolve)
		}
		w.Header().Set("X-Cache", "proxy")
		if sp.ServeAudio(w, r, id, lower) {
			return
		}
		s.log.Warn("stream 代理拉取音频失败", "source", source, "track", id)
		writeError(w, http.StatusBadGateway, "upstream", "这首曲目暂时取不到音频")
		return
	}

	t := model.Track{ID: id, Source: source}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := p.Resolve(ctx, &t); err != nil || strings.TrimSpace(t.URL) == "" {
		s.log.Warn("stream 解析播放地址失败", "source", source, "track", id, "err", err)
		writeError(w, http.StatusBadGateway, "upstream", "这首曲目暂时取不到音频")
		return
	}
	if s.cache != nil && s.cacheResolve != nil {
		s.cache.Ensure(source, id, s.cacheResolve)
	}
	w.Header().Set("X-Cache", "miss")
	http.Redirect(w, r, t.URL, http.StatusFound)
}

// handleCacheStats 给排查/巡检用：缓存了多少首、各账号各占多少。
func (s *Server) handleCacheStats(w http.ResponseWriter, _ *http.Request) {
	if s.cache == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, s.cache.Stats())
}

// decodeJSON 读取并解码 JSON body，限制 64KB 防止恶意大包。
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
