package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hakrelin/music/server/internal/config"
	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider/bilibili"
	"github.com/hakrelin/music/server/internal/qrcode"
)

const (
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
	mux.HandleFunc("GET /api/admin/netease/qr/poll", s.requireAdmin(s.handleAdminNeteaseQRPoll))

	mux.HandleFunc("GET /api/admin/bilibili/status", s.requireAdmin(s.handleAdminBiliStatus))
	mux.HandleFunc("POST /api/admin/bilibili/qr/create", s.requireAdmin(s.handleAdminBiliQRCreate))
	mux.HandleFunc("GET /api/admin/bilibili/qr/poll", s.requireAdmin(s.handleAdminBiliQRPoll))
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
	// 滑动续期
	s.adminSessions[tok] = now.Add(adminSessionTTL)
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

func (s *Server) clientIP(r *http.Request) string {
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xf != "" {
		if first, _, ok := strings.Cut(xf, ","); ok {
			return strings.TrimSpace(first)
		}
		return xf
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (s *Server) checkLoginRateLimit(ip string) bool {
	now := time.Now()
	cutoff := now.Add(-loginFailWindow)
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	raw := s.loginFails[ip]
	kept := raw[:0]
	for _, ts := range raw {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) == 0 {
		delete(s.loginFails, ip)
		return true
	}
	s.loginFails[ip] = kept
	return len(kept) < loginFailMaxCount
}

func (s *Server) recordLoginFailure(ip string) {
	now := time.Now()
	s.adminMu.Lock()
	if len(s.loginFails) > 512 {
		s.loginFails = make(map[string][]time.Time)
	}
	s.loginFails[ip] = append(s.loginFails[ip], now)
	s.adminMu.Unlock()
}

func (s *Server) clearLoginFailure(ip string) {
	s.adminMu.Lock()
	delete(s.loginFails, ip)
	s.adminMu.Unlock()
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
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
	ip := s.clientIP(r)
	if !s.checkLoginRateLimit(ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "尝试过于频繁，请 5 分钟后再试")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	s.cfgMu.RLock()
	want := s.cfg.RootPassword
	s.cfgMu.RUnlock()

	got := req.Password
	if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		s.recordLoginFailure(ip)
		writeError(w, http.StatusForbidden, "bad_password", "管理员口令不对")
		return
	}
	s.clearLoginFailure(ip)

	tok := newAdminToken()
	now := time.Now()
	s.adminMu.Lock()
	for k, exp := range s.adminSessions {
		if now.After(exp) {
			delete(s.adminSessions, k)
		}
	}
	s.adminSessions[tok] = now.Add(adminSessionTTL)
	s.adminMu.Unlock()

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

	var cacheStats map[string]any
	if s.cache != nil {
		cacheStats = s.cache.Stats()
	} else {
		cacheStats = map[string]any{"enabled": false}
	}

	return map[string]any{
		"version":         s.version,
		"secretsFile":     meta.SecretsPath,
		"secretsExists":   meta.SecretsExists,
		"secretsWritable": config.CheckWritable(meta.SecretsPath),
		"configFiles":     append([]string(nil), meta.ConfigFiles...),
		"envOverrides":    append([]string(nil), meta.EnvOverrides...),
		"providerMode":    cfg.Provider,
		"primaryProvider": s.hub.PrimaryProvider(),
		"sources":         s.sources(),
		"auth": map[string]any{
			"rootPasswordSet":  cfg.RootPassword != "",
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

// mutateSecrets 在持锁状态下原子修改并落盘 secrets.json，随后提交内存配置。
func (s *Server) mutateSecrets(fn func(sec *config.Secrets, cfg *config.Config) error) (config.Config, error) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

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
	return nextCfg.Clone(), nil
}

func (s *Server) applyNeteaseRuntime(cfg config.Config) {
	s.netease.SetCookie(cfg.NeteaseCookie)
	useWY := strings.EqualFold(cfg.Provider, "wy") ||
		(strings.EqualFold(cfg.Provider, "auto") && strings.TrimSpace(cfg.NeteaseCookie) != "")
	if useWY {
		s.reg.Register(s.netease)
		s.hub.SetPrimaryProvider("wy")
	} else {
		s.reg.Unregister("wy")
		s.hub.SetPrimaryProvider("demo")
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
	next := s.rebuildCache(cfg)
	s.cache.Swap(next)
	if next != nil && next.Enabled() {
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

	cfg, err := s.mutateSecrets(func(sec *config.Secrets, cfg *config.Config) error {
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
		writeError(w, http.StatusInternalServerError, "save_failed", "保存口令失败："+err.Error())
		return
	}
	s.hub.UpdatePasswords(cfg.RootPassword, cfg.AdminPassword)

	// 修改超管口令后清理其他已登录会话，保留当前操作者会话。
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
	cfg, err := s.mutateSecrets(func(sec *config.Secrets, cfg *config.Config) error {
		sec.Netease.Cookie = config.StrPtr(cookie)
		cfg.NeteaseCookie = cookie
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", "保存网易云 Cookie 失败："+err.Error())
		return
	}
	s.applyNeteaseRuntime(cfg)
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
	cfg, err := s.mutateSecrets(func(sec *config.Secrets, cfg *config.Config) error {
		if req.Enabled != nil {
			sec.Bilibili.Enabled = config.BoolPtr(*req.Enabled)
			cfg.Bilibili.Enabled = *req.Enabled
		}
		if req.UseLogin != nil {
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
		writeError(w, http.StatusInternalServerError, "save_failed", "保存 B 站配置失败："+err.Error())
		return
	}
	s.applyBilibiliRuntime(cfg)
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

	cfg, err := s.mutateSecrets(func(sec *config.Secrets, cfg *config.Config) error {
		oldAccs := cfg.Storage.Accounts
		nextAccs := make([]config.StorageAccount, 0, len(req.Accounts))
		for _, in := range req.Accounts {
			acc := config.StorageAccount{
				Label:      strings.TrimSpace(in.Label),
				Bucket:     strings.TrimSpace(in.Bucket),
				PublicBase: strings.TrimSpace(in.PublicBase),
				AccessKey:  strings.TrimSpace(in.AccessKey),
				SecretKey:  strings.TrimSpace(in.SecretKey),
			}
			// 已有账号留空密钥时沿用原值，避免修改其他字段时意外清空密钥。
			if in.ExistingIndex != nil && *in.ExistingIndex >= 0 && *in.ExistingIndex < len(oldAccs) {
				prev := oldAccs[*in.ExistingIndex]
				if acc.AccessKey == "" {
					acc.AccessKey = prev.AccessKey
				}
				if acc.SecretKey == "" {
					acc.SecretKey = prev.SecretKey
				}
			}
			if acc.Bucket == "" && acc.AccessKey == "" && acc.SecretKey == "" {
				continue
			}
			nextAccs = append(nextAccs, acc)
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
		writeError(w, http.StatusInternalServerError, "save_failed", "保存对象存储配置失败："+err.Error())
		return
	}
	s.applyStorageRuntime(cfg)
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
	_, err := s.mutateSecrets(func(sec *config.Secrets, cfg *config.Config) error {
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
			delete(sec.Keys, k)
			delete(cfg.Keys, k)
		}
		for k, v := range req.Set {
			k = strings.TrimSpace(k)
			if k == "" || len(k) > 64 {
				continue
			}
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			sec.Keys[k] = v
			cfg.Keys[k] = v
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", "保存自定义 Key 失败："+err.Error())
		return
	}
	s.log.Info("自定义 Key 配置已更新")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": s.buildAdminConfigSnapshot()})
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
	writeJSON(w, http.StatusOK, map[string]any{
		"key":          qr.Key,
		"url":          qr.URL,
		"qrSvgDataUrl": svgDataURL,
	})
}

func (s *Server) handleAdminNeteaseQRPoll(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "缺少二维码 key")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res, err := s.netease.PollLoginQR(ctx, key)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}
	out := map[string]any{
		"status":   res.Status,
		"code":     res.Code,
		"message":  res.Message,
		"nickname": res.Nickname,
		"avatar":   res.Avatar,
	}
	if res.Status == "confirmed" && res.Cookie != "" {
		cfg, serr := s.mutateSecrets(func(sec *config.Secrets, cfg *config.Config) error {
			sec.Netease.Cookie = config.StrPtr(res.Cookie)
			cfg.NeteaseCookie = res.Cookie
			return nil
		})
		if serr != nil {
			writeError(w, http.StatusInternalServerError, "save_failed", "扫码成功但保存 Cookie 失败："+serr.Error())
			return
		}
		s.applyNeteaseRuntime(cfg)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"key":          qr.Key,
		"url":          qr.URL,
		"qrSvgDataUrl": svgDataURL,
	})
}

func (s *Server) handleAdminBiliQRPoll(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "缺少二维码 key")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	res, err := s.bilibili.PollLoginQR(ctx, key)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}
	out := map[string]any{
		"status":  res.Status,
		"code":    res.Code,
		"message": res.Message,
	}
	if res.Status == "confirmed" && res.Cookie != "" {
		cfg, serr := s.mutateSecrets(func(sec *config.Secrets, cfg *config.Config) error {
			sec.Bilibili.Cookie = config.StrPtr(res.Cookie)
			cfg.Bilibili.Cookie = res.Cookie
			cfg.BilibiliCookie = res.Cookie
			return nil
		})
		if serr != nil {
			writeError(w, http.StatusInternalServerError, "save_failed", "扫码成功但保存 B 站 Cookie 失败："+serr.Error())
			return
		}
		s.applyBilibiliRuntime(cfg)
		s.log.Info("管理员通过扫码登录 B 站成功")
		out["saved"] = true
		out["config"] = s.buildAdminConfigSnapshot()
	}
	writeJSON(w, http.StatusOK, out)
}
