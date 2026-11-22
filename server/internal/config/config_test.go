package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSecretsLayeringAndExplicitClear(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "config.json")
	secPath := filepath.Join(dir, "secrets.json")

	baseJSON := `{
		"httpAddr": ":9090",
		"provider": "auto",
		"neteaseCookie": "MUSIC_U=old_cookie; __csrf=old",
		"bilibiliCookie": "SESSDATA=old_bili; bili_jct=abc",
		"rootPassword": "base-root",
		"adminPassword": "base-admin",
		"dataDir": "` + filepath.ToSlash(dir) + `",
		"secretsFile": "` + filepath.ToSlash(secPath) + `",
		"storage": {
			"enabled": true,
			"accounts": [{"label": "a1", "accessKey": "ak1", "secretKey": "sk1", "bucket": "b1"}]
		}
	}`
	if err := os.WriteFile(basePath, []byte(baseJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	// 1. 尚无 secrets.json 时兼容旧配置
	cfg, meta, err := LoadWithMeta(basePath)
	if err != nil {
		t.Fatalf("LoadWithMeta failed: %v", err)
	}
	if meta.SecretsExists {
		t.Fatal("secrets should not exist yet")
	}
	if !cfg.Bilibili.Enabled || !cfg.Bilibili.UseLogin {
		t.Fatalf("bilibili should default to enabled+useLogin: %+v", cfg.Bilibili)
	}
	if cfg.Bilibili.Cookie != "SESSDATA=old_bili; bili_jct=abc" {
		t.Fatalf("flat bilibiliCookie not mapped: %q", cfg.Bilibili.Cookie)
	}
	if cfg.NeteaseCookie != "MUSIC_U=old_cookie; __csrf=old" {
		t.Fatalf("unexpected netease cookie: %q", cfg.NeteaseCookie)
	}

	// 2. 写入 secrets.json：显式清空网易云 cookie，更新 B 站 cookie 和自定义 key
	sec := Secrets{
		Version: 1,
		Auth: AuthSecrets{
			RootPassword: "new-root-password",
		},
		Netease: NeteaseSecrets{
			Cookie: StrPtr(""), // 显式清空，不能被 base 的 old_cookie 复活
		},
		Bilibili: BilibiliSecrets{
			Enabled:  BoolPtr(true),
			UseLogin: BoolPtr(true),
			Cookie:   StrPtr("SESSDATA=fresh_sess; bili_jct=jct1; DedeUserID=42"),
		},
		Keys: map[string]string{
			"customApiKey": "secret-123456",
		},
	}
	if err := SaveSecrets(secPath, sec); err != nil {
		t.Fatalf("SaveSecrets failed: %v", err)
	}
	info, err := os.Stat(secPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("secrets file mode should be 0600, got %o", perm)
	}

	cfg2, meta2, err := LoadWithMeta(basePath)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if !meta2.SecretsExists {
		t.Fatal("secrets should exist after save")
	}
	if cfg2.RootPassword != "new-root-password" || cfg2.AdminPassword != "base-admin" {
		t.Fatalf("password merge mismatch: root=%q admin=%q", cfg2.RootPassword, cfg2.AdminPassword)
	}
	if cfg2.NeteaseCookie != "" {
		t.Fatalf("explicitly cleared netease cookie should stay empty, got %q", cfg2.NeteaseCookie)
	}
	if cfg2.Bilibili.Cookie != "SESSDATA=fresh_sess; bili_jct=jct1; DedeUserID=42" {
		t.Fatalf("bilibili cookie mismatch: %q", cfg2.Bilibili.Cookie)
	}
	if cfg2.Keys["customApiKey"] != "secret-123456" {
		t.Fatalf("custom key missing: %+v", cfg2.Keys)
	}
	// storage 未在 secrets.json 覆盖时应保留原 config.json 账号
	if !cfg2.Storage.Enabled || len(cfg2.Storage.Accounts) != 1 || cfg2.Storage.Accounts[0].Bucket != "b1" {
		t.Fatalf("base storage should remain intact: %+v", cfg2.Storage)
	}
}

func TestMaskAndCookieKeys(t *testing.T) {
	if got := MaskSecret("abcdefghijkl"); got != "abcd…ijkl" {
		t.Fatalf("MaskSecret mismatch: %q", got)
	}
	if got := MaskSecret("abc"); got != "***" {
		t.Fatalf("short MaskSecret mismatch: %q", got)
	}
	keys := CookieKeys("SESSDATA=abc; empty=; bili_jct=123; SESSDATA=dup; DedeUserID=9")
	want := []string{"DedeUserID", "SESSDATA", "bili_jct"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("CookieKeys = %v, want %v", keys, want)
	}
}
