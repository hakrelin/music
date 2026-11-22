package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hakrelin/music/server/internal/config"
	"github.com/hakrelin/music/server/internal/hub"
	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider"
	"github.com/hakrelin/music/server/internal/provider/bilibili"
	"github.com/hakrelin/music/server/internal/provider/demo"
	"github.com/hakrelin/music/server/internal/provider/netease"
	"github.com/hakrelin/music/server/internal/storage"
)

func newTestServer(t *testing.T) (*Server, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	secPath := filepath.Join(dir, "secrets.json")

	cfg := config.Default()
	cfg.Provider = "auto"
	cfg.RootPassword = "root-secret"
	cfg.AdminPassword = "admin-secret"
	cfg.DataDir = dir
	cfg.SecretsFile = secPath
	cfg.Normalize()

	reg := provider.NewRegistry()
	demoP := demo.New()
	reg.Register(demoP)
	wyP := netease.New(netease.Config{})
	biliP := bilibili.New(bilibili.Config{UseLogin: true})
	reg.Register(biliP)

	h := hub.New(hub.Options{
		Version:         "test",
		Registry:        reg,
		PrimaryProvider: "demo",
		RootPassword:    cfg.RootPassword,
		AdminPassword:   cfg.AdminPassword,
	})
	meta := config.Meta{
		SecretsPath: secPath,
		Secrets:     config.Secrets{Version: 1},
	}
	srv := New(Options{
		Config:   cfg,
		Meta:     meta,
		Hub:      h,
		Registry: reg,
		Demo:     demoP,
		Netease:  wyP,
		Bilibili: biliP,
		Cache:    storage.NewHolder(nil),
		Version:  "test",
	})
	return srv, srv.Handler(), secPath
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminAuthConfigPersistenceAndHotReload(t *testing.T) {
	srv, handler, secPath := newTestServer(t)

	// 未登录访问配置必须 401
	if rec := doJSON(t, handler, http.MethodGet, "/api/admin/config", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated config want 401, got %d", rec.Code)
	}

	// 错误口令返回 403
	if rec := doJSON(t, handler, http.MethodPost, "/api/admin/login", map[string]string{"password": "wrong"}); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong password want 403, got %d", rec.Code)
	}

	// 正确超管口令登录成功并下发 HttpOnly Cookie
	loginRec := doJSON(t, handler, http.MethodPost, "/api/admin/login", map[string]string{"password": "root-secret"})
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 || !cookies[0].HttpOnly {
		t.Fatalf("expected HttpOnly admin session cookie, got %+v", cookies)
	}

	// 初始 /api/config 应包含 demo（主源）和 bili
	pubRec := doJSON(t, handler, http.MethodGet, "/api/config", nil)
	if !strings.Contains(pubRec.Body.String(), `"id":"bili"`) || !strings.Contains(pubRec.Body.String(), `"id":"demo"`) {
		t.Fatalf("public config missing expected sources: %s", pubRec.Body.String())
	}

	// 配置网易云 Cookie → 立即落盘 secrets.json 且热切换到主源 wy
	wyRec := doJSON(t, handler, http.MethodPost, "/api/admin/config/netease", map[string]any{
		"cookie": "MUSIC_U=abc123456; __csrf=xyz999",
	}, cookies...)
	if wyRec.Code != http.StatusOK {
		t.Fatalf("save netease failed: %d %s", wyRec.Code, wyRec.Body.String())
	}
	if srv.hub.PrimaryProvider() != "wy" {
		t.Fatalf("expected primary provider hot-switched to wy, got %q", srv.hub.PrimaryProvider())
	}
	if srv.netease.Cookie() != "MUSIC_U=abc123456; __csrf=xyz999" {
		t.Fatalf("netease provider cookie not updated")
	}

	// 配置 B 站显式 Cookie + 保持登录态开关
	biliRec := doJSON(t, handler, http.MethodPost, "/api/admin/config/bilibili", map[string]any{
		"enabled":  true,
		"useLogin": true,
		"cookie":   "SESSDATA=bili-sess-123; bili_jct=jct1",
	}, cookies...)
	if biliRec.Code != http.StatusOK {
		t.Fatalf("save bilibili failed: %d %s", biliRec.Code, biliRec.Body.String())
	}
	if !srv.bilibili.UseLogin() || !strings.Contains(srv.bilibili.Cookie(), "SESSDATA=bili-sess-123") {
		t.Fatalf("bilibili runtime not updated")
	}

	// 配置自定义 Key 与存储账号，验证读取时脱敏且留空保留原密钥
	keyRec := doJSON(t, handler, http.MethodPost, "/api/admin/config/keys", map[string]any{
		"set": map[string]string{"openai_key": "sk-super-secret-value"},
	}, cookies...)
	if keyRec.Code != http.StatusOK {
		t.Fatalf("save keys failed: %d %s", keyRec.Code, keyRec.Body.String())
	}
	if strings.Contains(keyRec.Body.String(), "sk-super-secret-value") {
		t.Fatalf("raw custom key must never be echoed back: %s", keyRec.Body.String())
	}

	stRec := doJSON(t, handler, http.MethodPost, "/api/admin/config/storage", map[string]any{
		"enabled":         false,
		"maxMBPerAccount": 2048,
		"evictAfterDays":  30,
		"minHits":         2,
		"accounts": []map[string]any{
			{"label": "acc0", "bucket": "tk-audio-a", "publicBase": "https://a.example", "accessKey": "AK-ONE-12345", "secretKey": "SK-ONE-98765"},
		},
	}, cookies...)
	if stRec.Code != http.StatusOK {
		t.Fatalf("save storage failed: %d %s", stRec.Code, stRec.Body.String())
	}
	// 更新 publicBase 时不填密钥，原有 accessKey/secretKey 必须保留
	stRec2 := doJSON(t, handler, http.MethodPost, "/api/admin/config/storage", map[string]any{
		"enabled":         false,
		"maxMBPerAccount": 4096,
		"evictAfterDays":  0,
		"minHits":         2,
		"accounts": []map[string]any{
			{"existingIndex": 0, "label": "acc0", "bucket": "tk-audio-a", "publicBase": "https://b.example"},
		},
	}, cookies...)
	if stRec2.Code != http.StatusOK {
		t.Fatalf("save storage preserve failed: %d %s", stRec2.Code, stRec2.Body.String())
	}

	// 从磁盘重新读取 secrets.json 校验落盘内容完整
	sec, exists, err := config.LoadSecrets(secPath)
	if err != nil || !exists {
		t.Fatalf("LoadSecrets failed: exists=%v err=%v", exists, err)
	}
	if sec.Keys["openai_key"] != "sk-super-secret-value" {
		t.Fatalf("persisted key mismatch: %+v", sec.Keys)
	}
	if sec.Storage == nil || len(sec.Storage.Accounts) != 1 || sec.Storage.Accounts[0].SecretKey != "SK-ONE-98765" || sec.Storage.Accounts[0].PublicBase != "https://b.example" {
		t.Fatalf("persisted storage mismatch: %+v", sec.Storage)
	}

	// 退出登录后再访问应 401
	logoutRec := doJSON(t, handler, http.MethodPost, "/api/admin/logout", nil, cookies...)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout failed: %d", logoutRec.Code)
	}
	if rec := doJSON(t, handler, http.MethodGet, "/api/admin/config", nil, cookies...); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after logout want 401, got %d", rec.Code)
	}
}

func TestAdminPasswordRotationAndLoginRateLimit(t *testing.T) {
	_, handler, _ := newTestServer(t)

	// 两个独立会话同时登录
	rA := doJSON(t, handler, http.MethodPost, "/api/admin/login", map[string]string{"password": "root-secret"})
	rB := doJSON(t, handler, http.MethodPost, "/api/admin/login", map[string]string{"password": "root-secret"})
	cA := rA.Result().Cookies()
	cB := rB.Result().Cookies()

	// 会话 A 修改超管口令：会话 A 保持有效，会话 B 被立即吊销
	upd := doJSON(t, handler, http.MethodPost, "/api/admin/config/auth", map[string]string{
		"rootPassword":  "rotated-root",
		"adminPassword": "rotated-admin",
	}, cA...)
	if upd.Code != http.StatusOK {
		t.Fatalf("rotate auth failed: %d %s", upd.Code, upd.Body.String())
	}
	if rec := doJSON(t, handler, http.MethodGet, "/api/admin/config", nil, cA...); rec.Code != http.StatusOK {
		t.Fatalf("caller session should remain valid, got %d", rec.Code)
	}
	if rec := doJSON(t, handler, http.MethodGet, "/api/admin/config", nil, cB...); rec.Code != http.StatusUnauthorized {
		t.Fatalf("other session should be revoked after rootPassword change, got %d", rec.Code)
	}

	// 新口令可以登录，旧口令被拒且连续失败触发限流 429
	if rec := doJSON(t, handler, http.MethodPost, "/api/admin/login", map[string]string{"password": "rotated-root"}); rec.Code != http.StatusOK {
		t.Fatalf("new root password login failed: %d", rec.Code)
	}
	for i := 0; i < loginFailMaxCount; i++ {
		if rec := doJSON(t, handler, http.MethodPost, "/api/admin/login", map[string]string{"password": "wrong"}); rec.Code != http.StatusForbidden {
			t.Fatalf("fail #%d want 403, got %d", i+1, rec.Code)
		}
	}
	if rec := doJSON(t, handler, http.MethodPost, "/api/admin/login", map[string]string{"password": "rotated-root"}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 rate_limited after %d fails, got %d", loginFailMaxCount, rec.Code)
	}
}

type fakeStreamProxy struct {
	*demo.Provider
	lastID    string
	lastLower bool
}

func (f *fakeStreamProxy) Name() string { return "bili" }

func (f *fakeStreamProxy) ServeAudio(w http.ResponseWriter, r *http.Request, id string, lower bool) bool {
	f.lastID = id
	f.lastLower = lower
	w.Header().Set("Content-Type", "audio/mp4")
	w.Header().Set("Accept-Ranges", "bytes")
	if r.Header.Get("Range") == "bytes=0-3" {
		w.Header().Set("Content-Range", "bytes 0-3/8")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("M4A!"))
		return true
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("M4A!DATA"))
	return true
}

func (f *fakeStreamProxy) Resolve(_ context.Context, t *model.Track) error {
	t.URL = "/api/stream/" + t.ID + "?source=bili"
	return nil
}

func TestStreamRouteDispatchesToBiliAudioProxy(t *testing.T) {
	srv, handler, _ := newTestServer(t)
	proxy := &fakeStreamProxy{Provider: demo.New()}
	srv.reg.Register(proxy)

	req := httptest.NewRequest(http.MethodGet, "/api/stream/BV1xx411c7mD?source=bili&lower=1", nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Cache") != "proxy" {
		t.Fatalf("expected X-Cache=proxy, got %q", rec.Header().Get("X-Cache"))
	}
	if rec.Header().Get("Content-Type") != "audio/mp4" || rec.Body.String() != "M4A!" {
		t.Fatalf("unexpected response: ctype=%q body=%q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if proxy.lastID != "BV1xx411c7mD" || !proxy.lastLower {
		t.Fatalf("proxy args mismatch: id=%q lower=%v", proxy.lastID, proxy.lastLower)
	}
}

func TestAdminDefaultPasswordAndForwardedIP(t *testing.T) {
	srv, handler, _ := newTestServer(t)
	srv.cfg.RootPassword = config.DefaultRootPassword
	defaultLogin := doJSON(t, handler, http.MethodPost, "/api/admin/login", map[string]string{"password": config.DefaultRootPassword})
	if defaultLogin.Code != http.StatusServiceUnavailable || len(defaultLogin.Result().Cookies()) != 0 {
		t.Fatalf("default root password must not open /admin: %d", defaultLogin.Code)
	}
	srv.cfg.RootPassword = "root-secret"
	for i := 0; i <= loginFailMaxCount; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"password":"wrong"}`))
		req.RemoteAddr = "127.0.0.1:3000"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d, 198.51.100.4", i+1))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i < loginFailMaxCount && rec.Code != http.StatusForbidden || i == loginFailMaxCount && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("request %d with changing forwarded prefix returned %d", i+1, rec.Code)
		}
	}
}

func TestAdminEnvOverridesAndDeletionOfLegacyKeys(t *testing.T) {
	srv, handler, secPath := newTestServer(t)
	srv.cfg.Keys = map[string]string{"legacy": "from-main-config"}
	srv.cfg.Bilibili.UseLogin = true
	srv.meta.EnvOverrides = []string{"MUSIC_ROOT_PASSWORD", "MUSIC_BILIBILI_USE_LOGIN"}
	login := doJSON(t, handler, http.MethodPost, "/api/admin/login", map[string]string{"password": "root-secret"})
	cookies := login.Result().Cookies()
	for _, test := range []struct {
		path string
		body map[string]any
	}{
		{"/api/admin/config/auth", map[string]any{"rootPassword": "should-not-win"}},
		{"/api/admin/config/bilibili", map[string]any{"useLogin": false}},
	} {
		if rec := doJSON(t, handler, http.MethodPost, test.path, test.body, cookies...); rec.Code != http.StatusConflict {
			t.Fatalf("expected env override conflict: %s", rec.Body.String())
		}
	}
	if _, exists, err := config.LoadSecrets(secPath); exists || err != nil {
		t.Fatalf("rejected edit changed secrets: exists=%v err=%v", exists, err)
	}
	if rec := doJSON(t, handler, http.MethodPost, "/api/admin/config/bilibili", map[string]any{
		"useLogin": true, "cookie": "SESSDATA=local-test",
	}, cookies...); rec.Code != http.StatusOK {
		t.Fatalf("write of non-overridden fields failed: %s", rec.Body.String())
	}
	if rec := doJSON(t, handler, http.MethodPost, "/api/admin/config/keys", map[string]any{
		"delete": []string{"legacy"},
	}, cookies...); rec.Code != http.StatusOK {
		t.Fatalf("delete failed: %s", rec.Body.String())
	}
	sec, _, err := config.LoadSecrets(secPath)
	if err != nil {
		t.Fatal(err)
	}
	afterRestart := config.Config{Keys: map[string]string{" legacy ": "from-main-config"}}
	config.ApplySecrets(&afterRestart, sec)
	afterRestart.Normalize()
	if len(afterRestart.Keys) != 0 || sec.Bilibili.UseLogin != nil {
		t.Fatalf("legacy key was restored or env value was written to disk")
	}
}

func TestAdminQRScopeAndCrossOrigin(t *testing.T) {
	srv, handler, _ := newTestServer(t)
	login := func() *http.Cookie {
		return doJSON(t, handler, http.MethodPost, "/api/admin/login", map[string]string{"password": "root-secret"}).Result().Cookies()[0]
	}
	owner, other := login(), login()
	create := httptest.NewRequest(http.MethodPost, "/api/admin/bilibili/qr/create", nil)
	create.AddCookie(owner)
	if err := srv.publishAdminQR(create, "bili", "key-a"); err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/bilibili/qr/poll"
	body := map[string]string{"key": "key-a"}
	if rec := doJSON(t, handler, http.MethodPost, path, body, other); rec.Code != http.StatusConflict {
		t.Fatalf("another session polled the QR: %d", rec.Code)
	}
	if rec := doJSON(t, handler, http.MethodGet, path+"?key=key-a", nil, owner); rec.Code == http.StatusOK {
		t.Fatal("GET must not poll a QR")
	}
	crossOrigin := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"key":"key-a"}`))
	crossOrigin.AddCookie(owner)
	crossOrigin.Header.Set("Origin", "https://other.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, crossOrigin)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin poll returned %d", rec.Code)
	}
	old, err := srv.beginAdminQRPoll(create, "bili", "key-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.beginAdminQRPoll(create, "bili", "key-a"); err == nil {
		t.Fatal("parallel poll was allowed")
	}
	if err := srv.publishAdminQR(create, "bili", "key-b"); err != nil {
		t.Fatal(err)
	}
	srv.finishAdminQRPoll("bili", old, true)
	if _, err := srv.beginAdminQRPoll(create, "bili", "key-b"); err != nil {
		t.Fatalf("old poll invalidated replacement QR: %v", err)
	}
	if rec := doJSON(t, handler, http.MethodPost, "/api/admin/config/bilibili", map[string]bool{"clearCookie": true}, owner); rec.Code != http.StatusOK {
		t.Fatalf("clear cookie returned %d", rec.Code)
	}
	if _, err := srv.beginAdminQRPoll(create, "bili", "key-b"); err == nil {
		t.Fatal("clearing Cookie should invalidate the open QR")
	}
}

func TestAdminStorageProtectsExistingIndex(t *testing.T) {
	srv, handler, _ := newTestServer(t)
	srv.cfg.Storage.Accounts = []config.StorageAccount{
		{Bucket: "shared", AccessKey: "first", SecretKey: "first-secret"},
		{Bucket: "shared", AccessKey: "second", SecretKey: "second-secret"},
	}
	index := filepath.Join(srv.cfg.DataDir, "audio-cache.json")
	if err := os.WriteFile(index, []byte(`{"version":1,"items":{"wy:123":{"account":1,"key":"audio/wy/123.mp3"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cookies := doJSON(t, handler, http.MethodPost, "/api/admin/login", map[string]string{"password": "root-secret"}).Result().Cookies()
	first := map[string]any{"existingIndex": 0, "bucket": "shared"}
	second := map[string]any{"existingIndex": 1, "bucket": "shared"}
	for _, accounts := range [][]map[string]any{{second}, {second, first}} {
		if rec := doJSON(t, handler, http.MethodPost, "/api/admin/config/storage", map[string]any{
			"accounts": accounts,
		}, cookies...); rec.Code != http.StatusConflict {
			t.Fatalf("index-changing edit returned %d: %s", rec.Code, rec.Body.String())
		}
	}
	second["secretKey"] = "rotated"
	if rec := doJSON(t, handler, http.MethodPost, "/api/admin/config/storage", map[string]any{
		"accounts": []map[string]any{first, second},
	}, cookies...); rec.Code != http.StatusOK {
		t.Fatalf("in-place SecretKey update returned %d: %s", rec.Code, rec.Body.String())
	}
}
