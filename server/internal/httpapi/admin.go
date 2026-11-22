package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hakrelin/music/server/internal/config"
	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider/bilibili"
	"github.com/hakrelin/music/server/internal/qrcode"
	"github.com/hakrelin/music/server/internal/storage"
)

type loginAttempts struct {
	count int
	until time.Time
}

const (
	adminQRDuration   = 5 * time.Minute
	adminCookieName   = "musicd_admin"
	adminSessionTTL   = 12 * time.Hour
	loginFailWindow   = 5 * time.Minute
	loginFailMaxCount = 10
)

func (s *Server) registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/session", s.handleAdminSession)
	mux.HandleFunc("POST /api/admin/login", s.handleAdminLogin)
	mux.HandleFunc("POST /api/admin/logout", s.handleAdminLogout)

	mux.HandleFunc("GET /api/admin/config", s.requireAdmin(s.handleAdminConfig))
	mux.HandleFunc("POST /api/admin/config/auth", s.requireAdmin(s.handleAdminSaveAuth))
	mux.HandleFunc("POST /api/admin/config/netease", s.requireAdmin(s.handleAdminSaveNetease))
	mux.HandleFunc("POST /api/admin/config/bilibili", s.requireAdmin(s.handleAdminSaveBilibili))
	mux.HandleFunc("POST /api/admin/config/storage", s.requireAdmin(s.handleAdminSaveStorage))
	mux.HandleFunc("POST /api/admin/config/keys", s.requireAdmin(s.handleAdminSaveKeys))

	mux.HandleFunc("GET /api/admin/netease/status", s.requireAdmin(s.handleAdminNeteaseStatus))
	mux.HandleFunc("POST /api/admin/netease/qr/create", s.requireAdmin(s.handleAdminNeteaseQRCreate))
	mux.HandleFunc("POST /api/admin/netease/qr/poll", s.requireAdmin(s.handleAdminNeteaseQRPoll))

	mux.HandleFunc("GET /api/admin/bilibili/status", s.requireAdmin(s.handleAdminBiliStatus))
	mux.HandleFunc("POST /api/admin/bilibili/qr/create", s.requireAdmin(s.handleAdminBiliQRCreate))
	mux.HandleFunc("POST /api/admin/bilibili/qr/poll", s.requireAdmin(s.handleAdminBiliQRPoll))
}

// ---- 会话与鉴权 ----

func (s *Server) adminTokenFromReq(r *http.Request) string {
	c, err := r.Cookie(adminCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}

func (s *Server) isAdminAuthed(r *http.Request) bool {
	tok := s.adminTokenFromReq(r)
	if tok == "" {
		return false
	}
	now := time.Now()
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	exp, ok := s.adminSessions[tok]
	if !ok || now.After(exp) {
		delete(s.adminSessions, tok)
		return false
	}
	return true
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAdminAuthed(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录管理员后台")
			return
		}
		next(w, r)
	}
}

// 仅信任本机反代追加的最后一个地址，与 nginx 的 proxy_add_x_forwarded_for 对应。
func adminClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer != nil && peer.IsLoopback() {
		chain := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
		last := strings.TrimSpace(chain[strings.LastIndex(chain, ",")+1:])
		if ip := net.ParseIP(last); ip != nil {
			return ip.String()
		}
	}
	if peer != nil {
		return peer.String()
	}
	return host
}

func (s *Server) loginAllowedLocked(ip string, now time.Time) bool {
	for addr, last := range s.loginFails {
		if !last.until.After(now) {
			delete(s.loginFails, addr)
		}
	}
	attempt, exists := s.loginFails[ip]
	if exists {
		return attempt.count < loginFailMaxCount
	}
	return len(s.loginFails) < 4096
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return net.ParseIP(host).IsLoopback() && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func newAdminToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	authed := s.isAdminAuthed(r)
	resp := map[string]any{
		"authenticated": authed,
		"version":       s.version,
	}
	if authed {
		s.cfgMu.RLock()
		resp["secretsFile"] = s.meta.SecretsPath
		resp["secretsWritable"] = config.CheckWritable(s.meta.SecretsPath)
		resp["configFiles"] = append([]string(nil), s.meta.ConfigFiles...)
		resp["envOverrides"] = append([]string(nil), s.meta.EnvOverrides...)
		s.cfgMu.RUnlock()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	ip := adminClientIP(r)
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}

	// 口令读取和签发会话必须与口令修改串行，避免旧口令在吊销后签出新会话。
	s.cfgMu.RLock()
	want := s.cfg.RootPassword
	now := time.Now()
	s.adminMu.Lock()
	if want == "" || want == config.DefaultRootPassword {
		s.adminMu.Unlock()
		s.cfgMu.RUnlock()
		writeError(w, http.StatusServiceUnavailable, "admin_disabled", "请先在服务器配置中设置专用的 rootPassword")
		return
	}
	if !s.loginAllowedLocked(ip, now) {
		s.adminMu.Unlock()
		s.cfgMu.RUnlock()
		writeError(w, http.StatusTooManyRequests, "rate_limited", "尝试过于频繁，请稍后再试")
		return
	}
	gotHash, wantHash := sha256.Sum256([]byte(req.Password)), sha256.Sum256([]byte(want))
	if subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) != 1 {
		attempt := s.loginFails[ip]
		if attempt.count == 0 {
			attempt.until = now.Add(loginFailWindow)
		}
		attempt.count++
		s.loginFails[ip] = attempt
		s.adminMu.Unlock()
		s.cfgMu.RUnlock()
		writeError(w, http.StatusForbidden, "bad_password", "管理员口令不对")
		return
	}
	delete(s.loginFails, ip)
	tok := newAdminToken()
	for k, exp := range s.adminSessions {
		if now.After(exp) {
			delete(s.adminSessions, k)
		}
	}
	s.adminSessions[tok] = now.Add(adminSessionTTL)
	s.adminMu.Unlock()
	s.cfgMu.RUnlock()

	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    tok,
		Path:     "/api/admin",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(adminSessionTTL.Seconds()),
	})
	s.log.Info("管理员登录后台成功", "ip", ip)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if tok := s.adminTokenFromReq(r); tok != "" {
		s.adminMu.Lock()
		delete(s.adminSessions, tok)
		s.adminMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    "",
		Path:     "/api/admin",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- 配置读写与热更新 ----

type adminStorageAccountOut struct {
	Index           int    `json:"index"`
	Label           string `json:"label"`
	Bucket          string `json:"bucket"`
	PublicBase      string `json:"publicBase"`
	AccessKeyMasked string `json:"accessKeyMasked"`
	HasAccessKey    bool   `json:"hasAccessKey"`
	SecretKeyMasked string `json:"secretKeyMasked"`
	HasSecretKey    bool   `json:"hasSecretKey"`
}

type adminKeyOut struct {
	Key    string `json:"key"`
	Masked string `json:"masked"`
	Length int    `json:"length"`
}

func (s *Server) buildAdminConfigSnapshot() map[string]any {
	s.cfgMu.RLock()
	cfg := s.cfg.Clone()
	meta := s.meta
	primary, sources, cacheStats := s.hub.PrimaryProvider(), s.sources(), s.cache.Stats()
	s.cfgMu.RUnlock()

	accs := make([]adminStorageAccountOut, 0, len(cfg.Storage.Accounts))
	for i, a := range cfg.Storage.Accounts {
		accs = append(accs, adminStorageAccountOut{
			Index:           i,
			Label:           a.Label,
			Bucket:          a.Bucket,
			PublicBase:      a.PublicBase,
			AccessKeyMasked: config.MaskSecret(a.AccessKey),
			HasAccessKey:    a.AccessKey != "",
			SecretKeyMasked: config.MaskSecret(a.SecretKey),
			HasSecretKey:    a.SecretKey != "",
		})
	}

	keys := make([]adminKeyOut, 0, len(cfg.Keys))
	for k, v := range cfg.Keys {
		keys = append(keys, adminKeyOut{
			Key:    k,
			Masked: config.MaskSecret(v),
			Length: utf8.RuneCountInString(v),
		})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Key < keys[j].Key })

	return map[string]any{
		"version":         s.version,
		"secretsFile":     meta.SecretsPath,
		"secretsExists":   meta.SecretsExists,
		"secretsWritable": config.CheckWritable(meta.SecretsPath),
		"configFiles":     append([]string(nil), meta.ConfigFiles...),
		"envOverrides":    append([]string(nil), meta.EnvOverrides...),
		"providerMode":    cfg.Provider,
		"primaryProvider": primary,
		"sources":         sources,
		"auth": map[string]any{
			"rootPasswordSet":  cfg.RootPassword != "" && cfg.RootPassword != config.DefaultRootPassword,
			"adminPasswordSet": cfg.AdminPassword != "",
		},
		"netease": map[string]any{
			"configured":   cfg.NeteaseCookie != "",
			"cookieMasked": config.MaskSecret(cfg.NeteaseCookie),
			"cookieKeys":   config.CookieKeys(cfg.NeteaseCookie),
		},
		"bilibili": map[string]any{
			"enabled":      cfg.Bilibili.Enabled,
			"useLogin":     cfg.Bilibili.UseLogin,
			"configured":   cfg.Bilibili.Cookie != "",
			"cookieMasked": config.MaskSecret(cfg.Bilibili.Cookie),
			"cookieKeys":   config.CookieKeys(cfg.Bilibili.Cookie),
		},
		"storage": map[string]any{
			"enabled":         cfg.Storage.Enabled,
			"maxMBPerAccount": cfg.Storage.MaxMBPerAccount,
			"evictAfterDays":  cfg.Storage.EvictAfterDays,
			"minHits":         cfg.Storage.MinHits,
			"accounts":        accs,
		},
		"keys":       keys,
		"cacheStats": cacheStats,
	}
}

func (s *Server) handleAdminConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.buildAdminConfigSnapshot())
}

type adminSaveError struct {
	status  int
	code    string
	message string
}

func (e *adminSaveError) Error() string { return e.message }

func writeAdminSaveError(w http.ResponseWriter, message string, err error) {
	var validation *adminSaveError
	if errors.As(err, &validation) {
		writeError(w, validation.status, validation.code, validation.message)
		return
	}
	writeError(w, http.StatusInternalServerError, "save_failed", message+"："+err.Error())
}

// mutateSecrets 按同一顺序提交磁盘配置、内存配置和运行时状态。
func (s *Server) mutateSecrets(r *http.Request, apply func(config.Config), fn func(sec *config.Secrets, cfg *config.Config) error) (config.Config, error) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if err := r.Context().Err(); err != nil {
		return config.Config{}, err
	}
	if !s.isAdminAuthed(r) {
		return config.Config{}, &adminSaveError{http.StatusUnauthorized, "unauthorized", "请先登录管理员后台"}
	}

	nextSec := s.meta.Secrets.Clone()
	nextCfg := s.cfg.Clone()
	if err := fn(&nextSec, &nextCfg); err != nil {
		return config.Config{}, err
	}
	nextCfg.Normalize()

	if err := config.SaveSecrets(s.meta.SecretsPath, nextSec); err != nil {
		return config.Config{}, err
	}
	s.meta.Secrets = nextSec
	s.meta.SecretsExists = true
	s.cfg = nextCfg
	if apply != nil {
		apply(nextCfg)
	}
	return nextCfg, nil
}

func (s *Server) checkEnvOverride(key string, editing bool) error {
	if editing && slices.Contains(s.meta.EnvOverrides, key) {
		return &adminSaveError{http.StatusConflict, "env_override", key + " 由环境变量控制，请在服务器上修改"}
	}
	return nil
}

func (s *Server) applyNeteaseRuntime(cfg config.Config) {
	s.netease.SetCookie(cfg.NeteaseCookie)
	useWY := strings.EqualFold(cfg.Provider, "wy") ||
		(strings.EqualFold(cfg.Provider, "auto") && strings.TrimSpace(cfg.NeteaseCookie) != "")
	if useWY {
		s.reg.Register(s.netease)
		s.hub.SetPrimaryProvider("wy")
	} else {
		s.hub.SetPrimaryProvider("demo")
		s.reg.Unregister("wy")
	}
	s.hub.ClearURLCache("wy")
}

func (s *Server) applyBilibiliRuntime(cfg config.Config) {
	s.bilibili.UpdateConfig(bilibili.Config{
		Cookie:   cfg.Bilibili.Cookie,
		UseLogin: cfg.Bilibili.UseLogin,
	})
	if cfg.Bilibili.Enabled {
		s.reg.Register(s.bilibili)
	} else {
		s.reg.Unregister("bili")
	}
	s.hub.ClearURLCache("bili")
}

func (s *Server) applyStorageRuntime(cfg config.Config) {
	if s.cache == nil || s.rebuildCache == nil {
		return
	}
	s.cache.Replace(func() *storage.Cache { return s.rebuildCache(cfg) })
	s.hub.ClearURLCache("")
	if s.cache.Enabled() {
		s.hub.SetCacheCallbacks(
			func(t model.Track) { s.cache.Ensure(t.Source, t.ID, s.cacheResolve) },
			s.cache.Lookup,
		)
	} else {
		s.hub.SetCacheCallbacks(nil, nil)
	}
}

func (s *Server) handleAdminSaveAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RootPassword  string `json:"rootPassword"`
		AdminPassword string `json:"adminPassword"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	root := strings.TrimSpace(req.RootPassword)
	admin := strings.TrimSpace(req.AdminPassword)
	if root == "" && admin == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "请至少填写一项要修改的口令")
		return
	}
	if root != "" && utf8.RuneCountInString(root) > 64 {
		writeError(w, http.StatusBadRequest, "bad_request", "超级管理员口令不能超过 64 字")
		return
	}
	if admin != "" && utf8.RuneCountInString(admin) > 64 {
		writeError(w, http.StatusBadRequest, "bad_request", "房间管理员口令不能超过 64 字")
		return
	}

	if root == config.DefaultRootPassword {
		writeError(w, http.StatusBadRequest, "bad_request", "请为超管设置专用口令")
		return
	}
	_, err := s.mutateSecrets(r, func(cfg config.Config) {
		s.hub.UpdatePasswords(cfg.RootPassword, cfg.AdminPassword)
		if root != "" {
			curTok := s.adminTokenFromReq(r)
			s.adminMu.Lock()
			for k := range s.adminSessions {
				if k != curTok {
					delete(s.adminSessions, k)
				}
			}
			s.adminMu.Unlock()
		}
	}, func(sec *config.Secrets, cfg *config.Config) error {
		if err := s.checkEnvOverride("MUSIC_ROOT_PASSWORD", root != ""); err != nil {
			return err
		}
		if err := s.checkEnvOverride("MUSIC_ADMIN_PASSWORD", admin != ""); err != nil {
			return err
		}
		if root != "" {
			sec.Auth.RootPassword = root
			cfg.RootPassword = root
		}
		if admin != "" {
			sec.Auth.AdminPassword = admin
			cfg.AdminPassword = admin
		}
		return nil
	})
	if err != nil {
		writeAdminSaveError(w, "保存口令失败", err)
		return
	}
	s.log.Info("管理员口令已更新")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": s.buildAdminConfigSnapshot()})
}

func (s *Server) handleAdminSaveNetease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cookie string `json:"cookie"`
		Clear  bool   `json:"clear"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	cookie := config.SanitizeCookie(req.Cookie)
	if !req.Clear && cookie == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Cookie 不能为空（如需清空请点击清空按钮）")
		return
	}
	if req.Clear {
		cookie = ""
	}
	cfg, err := s.mutateSecrets(r, func(cfg config.Config) {
		delete(s.adminQRs, "wy")
		s.applyNeteaseRuntime(cfg)
	}, func(sec *config.Secrets, cfg *config.Config) error {
		if err := s.checkEnvOverride("MUSIC_NETEASE_COOKIE", true); err != nil {
			return err
		}
		sec.Netease.Cookie = config.StrPtr(cookie)
		cfg.NeteaseCookie = cookie
		return nil
	})
	if err != nil {
		writeAdminSaveError(w, "保存网易云 Cookie 失败", err)
		return
	}
	s.log.Info("网易云 Cookie 已更新", "configured", cfg.NeteaseCookie != "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": s.buildAdminConfigSnapshot()})
}

func (s *Server) handleAdminSaveBilibili(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled     *bool  `json:"enabled"`
		UseLogin    *bool  `json:"useLogin"`
		Cookie      string `json:"cookie"`
		ClearCookie bool   `json:"clearCookie"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	cookie := config.SanitizeCookie(req.Cookie)
	cfg, err := s.mutateSecrets(r, func(cfg config.Config) {
		if req.ClearCookie || cookie != "" {
			delete(s.adminQRs, "bili")
		}
		s.applyBilibiliRuntime(cfg)
	}, func(sec *config.Secrets, cfg *config.Config) error {
		if err := s.checkEnvOverride("MUSIC_BILIBILI_COOKIE", req.ClearCookie || cookie != ""); err != nil {
			return err
		}
		if err := s.checkEnvOverride("MUSIC_BILIBILI_ENABLED", req.Enabled != nil && *req.Enabled != cfg.Bilibili.Enabled); err != nil {
			return err
		}
		if err := s.checkEnvOverride("MUSIC_BILIBILI_USE_LOGIN", req.UseLogin != nil && *req.UseLogin != cfg.Bilibili.UseLogin); err != nil {
			return err
		}
		if req.Enabled != nil && !slices.Contains(s.meta.EnvOverrides, "MUSIC_BILIBILI_ENABLED") {
			sec.Bilibili.Enabled = config.BoolPtr(*req.Enabled)
			cfg.Bilibili.Enabled = *req.Enabled
		}
		if req.UseLogin != nil && !slices.Contains(s.meta.EnvOverrides, "MUSIC_BILIBILI_USE_LOGIN") {
			sec.Bilibili.UseLogin = config.BoolPtr(*req.UseLogin)
			cfg.Bilibili.UseLogin = *req.UseLogin
		}
		if req.ClearCookie {
			sec.Bilibili.Cookie = config.StrPtr("")
			cfg.Bilibili.Cookie = ""
			cfg.BilibiliCookie = ""
		} else if cookie != "" {
			sec.Bilibili.Cookie = config.StrPtr(cookie)
			cfg.Bilibili.Cookie = cookie
			cfg.BilibiliCookie = cookie
		}
		return nil
	})
	if err != nil {
		writeAdminSaveError(w, "保存 B 站配置失败", err)
		return
	}
	s.log.Info("B站配置已更新", "enabled", cfg.Bilibili.Enabled, "useLogin", cfg.Bilibili.UseLogin, "configured", cfg.Bilibili.Cookie != "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": s.buildAdminConfigSnapshot()})
}

func (s *Server) handleAdminSaveStorage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled         bool `json:"enabled"`
		MaxMBPerAccount int  `json:"maxMBPerAccount"`
		EvictAfterDays  int  `json:"evictAfterDays"`
		MinHits         int  `json:"minHits"`
		Accounts        []struct {
			ExistingIndex *int   `json:"existingIndex"`
			Label         string `json:"label"`
			Bucket        string `json:"bucket"`
			PublicBase    string `json:"publicBase"`
			AccessKey     string `json:"accessKey"`
			SecretKey     string `json:"secretKey"`
		} `json:"accounts"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}

	cfg, err := s.mutateSecrets(r, s.applyStorageRuntime, func(sec *config.Secrets, cfg *config.Config) error {
		oldAccs := cfg.Storage.Accounts
		nextAccs := make([]config.StorageAccount, 0, len(req.Accounts))
		used := make(map[int]bool)
		for _, in := range req.Accounts {
			acc := config.StorageAccount{
				Label:      strings.TrimSpace(in.Label),
				Bucket:     strings.TrimSpace(in.Bucket),
				PublicBase: strings.TrimSpace(in.PublicBase),
				AccessKey:  strings.TrimSpace(in.AccessKey),
				SecretKey:  strings.TrimSpace(in.SecretKey),
			}
			if in.ExistingIndex != nil {
				idx := *in.ExistingIndex
				if idx < 0 || idx >= len(oldAccs) || used[idx] {
					return &adminSaveError{http.StatusBadRequest, "bad_request", "存储账号序号无效，请刷新配置"}
				}
				used[idx] = true
				prev := oldAccs[idx]
				if acc.AccessKey == "" {
					acc.AccessKey = prev.AccessKey
				}
				if acc.SecretKey == "" {
					acc.SecretKey = prev.SecretKey
				}
			}
			if acc.Bucket == "" || acc.AccessKey == "" || acc.SecretKey == "" {
				return &adminSaveError{http.StatusBadRequest, "bad_request", "请填写完整的存储空间和账号密钥"}
			}
			if acc.PublicBase != "" {
				base, err := url.Parse(acc.PublicBase)
				if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Hostname() == "" ||
					base.User != nil || base.RawQuery != "" || base.Fragment != "" {
					return &adminSaveError{http.StatusBadRequest, "bad_request", "加速地址需为有效的 HTTP(S) 地址"}
				}
			}
			nextAccs = append(nextAccs, acc)
		}
		// 旧索引只记录账号数组序号。已启用或磁盘上仍有索引时保留现有账号的绑定。
		hasIndex := s.cache.Enabled()
		if cfg.DataDir != "" {
			_, err := os.Stat(filepath.Join(cfg.DataDir, "audio-cache.json"))
			hasIndex = hasIndex || !errors.Is(err, os.ErrNotExist)
		}
		if hasIndex {
			for i, prev := range oldAccs {
				if i >= len(nextAccs) || nextAccs[i].AccessKey != prev.AccessKey || nextAccs[i].Bucket != prev.Bucket {
					return &adminSaveError{http.StatusConflict, "storage_index", "现有缓存绑定账号序号、AccessKey 和存储空间，已有账号须保留绑定，可在末尾添加新账号"}
				}
			}
		}
		if req.Enabled && len(nextAccs) == 0 {
			return &adminSaveError{http.StatusBadRequest, "bad_request", "开启对象存储前请配置至少一个账号"}
		}
		if req.MaxMBPerAccount < 0 || req.MaxMBPerAccount > 1<<40 ||
			req.EvictAfterDays < 0 || req.EvictAfterDays > 36500 || req.MinHits < 0 {
			return &adminSaveError{http.StatusBadRequest, "bad_request", "对象存储淘汰参数超出范围"}
		}
		st := config.ObjectStorage{
			Enabled:         req.Enabled,
			Accounts:        nextAccs,
			MaxMBPerAccount: max(0, req.MaxMBPerAccount),
			EvictAfterDays:  max(0, req.EvictAfterDays),
			MinHits:         max(0, req.MinHits),
		}
		sec.Storage = &st
		cfg.Storage = st
		return nil
	})
	if err != nil {
		writeAdminSaveError(w, "保存对象存储配置失败", err)
		return
	}
	s.log.Info("对象存储配置已更新", "enabled", cfg.Storage.Enabled, "accounts", len(cfg.Storage.Accounts))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": s.buildAdminConfigSnapshot()})
}

func (s *Server) handleAdminSaveKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Set    map[string]string `json:"set"`
		Delete []string          `json:"delete"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	if len(req.Set) == 0 && len(req.Delete) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "没有要更新或删除的 Key")
		return
	}
	_, err := s.mutateSecrets(r, nil, func(sec *config.Secrets, cfg *config.Config) error {
		if sec.Keys == nil {
			sec.Keys = make(map[string]string)
		}
		if cfg.Keys == nil {
			cfg.Keys = make(map[string]string)
		}
		for _, k := range req.Delete {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if _, exists := cfg.Keys[k]; exists {
				delete(sec.Keys, k)
				delete(cfg.Keys, k)
				sec.DeletedKeys = append(sec.DeletedKeys, k)
			}
		}
		for k, v := range req.Set {
			k = strings.TrimSpace(k)
			if k == "" || len(k) > 64 || strings.TrimSpace(v) == "" {
				return &adminSaveError{http.StatusBadRequest, "bad_request", "Key 名称需为 1–64 字节，值不能为空"}
			}
			sec.DeletedKeys = slices.DeleteFunc(sec.DeletedKeys, func(deleted string) bool { return deleted == k })
			sec.Keys[k] = v
			cfg.Keys[k] = v
		}
		return nil
	})
	if err != nil {
		writeAdminSaveError(w, "保存自定义 Key 失败", err)
		return
	}
	s.log.Info("自定义 Key 配置已更新")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": s.buildAdminConfigSnapshot()})
}

// 每个音源只接受最近签发给当前会话的二维码。
type adminQR struct {
	key     string
	owner   string
	expires time.Time
	polling bool
}

func staleQR() *adminSaveError {
	return &adminSaveError{http.StatusConflict, "stale_qr", "二维码已失效，请重新生成"}
}

func (s *Server) publishAdminQR(r *http.Request, source, key string) error {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if err := r.Context().Err(); err != nil {
		return err
	}
	if !s.isAdminAuthed(r) {
		return &adminSaveError{http.StatusUnauthorized, "unauthorized", "请先登录管理员后台"}
	}
	s.adminQRs[source] = &adminQR{key: key, owner: s.adminTokenFromReq(r), expires: time.Now().Add(adminQRDuration)}
	return nil
}

func (s *Server) beginAdminQRPoll(r *http.Request, source, key string) (*adminQR, error) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	qr := s.adminQRs[source]
	if qr == nil || qr.key != key || qr.owner != s.adminTokenFromReq(r) || time.Now().After(qr.expires) {
		return nil, staleQR()
	}
	if qr.polling {
		return nil, &adminSaveError{http.StatusConflict, "qr_busy", "正在查询扫码状态"}
	}
	qr.polling = true
	return qr, nil
}

func (s *Server) finishAdminQRPoll(source string, qr *adminQR, expired bool) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if s.adminQRs[source] == qr {
		if expired {
			delete(s.adminQRs, source)
		}
		qr.polling = false
	}
}

// ---- 网易云扫码登录与状态检查 ----

func (s *Server) handleAdminNeteaseStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	info, err := s.netease.AccountStatus(ctx)
	if err != nil {
		s.log.Warn("检查网易云登录态失败", "err", err)
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleAdminNeteaseQRCreate(w http.ResponseWriter, r *http.Request) {
	s.cfgMu.RLock()
	override := s.checkEnvOverride("MUSIC_NETEASE_COOKIE", true)
	s.cfgMu.RUnlock()
	if override != nil {
		writeAdminSaveError(w, "生成二维码失败", override)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	qr, err := s.netease.CreateLoginQR(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}
	svgDataURL, err := qrcode.SVGDataURL(qr.URL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "qr_failed", "生成二维码图片失败："+err.Error())
		return
	}
	if err := s.publishAdminQR(r, "wy", qr.Key); err != nil {
		writeAdminSaveError(w, "生成二维码失败", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key":          qr.Key,
		"url":          qr.URL,
		"qrSvgDataUrl": svgDataURL,
	})
}

func (s *Server) handleAdminNeteaseQRPoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Key) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "缺少二维码 key")
		return
	}
	qr, err := s.beginAdminQRPoll(r, "wy", strings.TrimSpace(req.Key))
	if err != nil {
		writeAdminSaveError(w, "查询扫码状态失败", err)
		return
	}
	expired := false
	defer func() { s.finishAdminQRPoll("wy", qr, expired) }()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res, err := s.netease.PollLoginQR(ctx, req.Key)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}
	expired = res.Status == "expired"
	out := map[string]any{
		"status":   res.Status,
		"code":     res.Code,
		"message":  res.Message,
		"nickname": res.Nickname,
		"avatar":   res.Avatar,
	}
	if res.Status == "confirmed" && res.Cookie != "" {
		_, serr := s.mutateSecrets(r, func(cfg config.Config) {
			delete(s.adminQRs, "wy")
			s.applyNeteaseRuntime(cfg)
		}, func(sec *config.Secrets, cfg *config.Config) error {
			if s.adminQRs["wy"] != qr || time.Now().After(qr.expires) {
				return staleQR()
			}
			sec.Netease.Cookie = config.StrPtr(res.Cookie)
			cfg.NeteaseCookie = res.Cookie
			return nil
		})
		if serr != nil {
			writeAdminSaveError(w, "扫码成功但保存 Cookie 失败", serr)
			return
		}
		s.log.Info("管理员通过扫码登录网易云成功")
		out["saved"] = true
		out["config"] = s.buildAdminConfigSnapshot()
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- B 站扫码登录与状态检查 ----

func (s *Server) handleAdminBiliStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	info, err := s.bilibili.AccountStatus(ctx)
	if err != nil {
		s.log.Warn("检查 B 站登录态失败", "err", err)
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleAdminBiliQRCreate(w http.ResponseWriter, r *http.Request) {
	s.cfgMu.RLock()
	override := s.checkEnvOverride("MUSIC_BILIBILI_COOKIE", true)
	s.cfgMu.RUnlock()
	if override != nil {
		writeAdminSaveError(w, "生成二维码失败", override)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	qr, err := s.bilibili.CreateLoginQR(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}
	svgDataURL, err := qrcode.SVGDataURL(qr.URL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "qr_failed", "生成二维码图片失败："+err.Error())
		return
	}
	if err := s.publishAdminQR(r, "bili", qr.Key); err != nil {
		writeAdminSaveError(w, "生成二维码失败", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key":          qr.Key,
		"url":          qr.URL,
		"qrSvgDataUrl": svgDataURL,
	})
}

func (s *Server) handleAdminBiliQRPoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Key) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "缺少二维码 key")
		return
	}
	qr, err := s.beginAdminQRPoll(r, "bili", strings.TrimSpace(req.Key))
	if err != nil {
		writeAdminSaveError(w, "查询扫码状态失败", err)
		return
	}
	expired := false
	defer func() { s.finishAdminQRPoll("bili", qr, expired) }()
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	res, err := s.bilibili.PollLoginQR(ctx, req.Key)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}
	expired = res.Status == "expired"
	out := map[string]any{
		"status":  res.Status,
		"code":    res.Code,
		"message": res.Message,
	}
	if res.Status == "confirmed" && res.Cookie != "" {
		_, serr := s.mutateSecrets(r, func(cfg config.Config) {
			delete(s.adminQRs, "bili")
			s.applyBilibiliRuntime(cfg)
		}, func(sec *config.Secrets, cfg *config.Config) error {
			if s.adminQRs["bili"] != qr || time.Now().After(qr.expires) {
				return staleQR()
			}
			sec.Bilibili.Cookie = config.StrPtr(res.Cookie)
			cfg.Bilibili.Cookie = res.Cookie
			cfg.BilibiliCookie = res.Cookie
			return nil
		})
		if serr != nil {
			writeAdminSaveError(w, "扫码成功但保存 B 站 Cookie 失败", serr)
			return
		}
		s.log.Info("管理员通过扫码登录 B 站成功")
		out["saved"] = true
		out["config"] = s.buildAdminConfigSnapshot()
	}
	writeJSON(w, http.StatusOK, out)
}
