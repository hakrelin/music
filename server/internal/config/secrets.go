package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Secrets 是独立的敏感配置文件结构（默认 secrets.json / server/secrets.local.json），
// 集中管理管理员口令、网易云 / B站 Cookie、多吉云对象存储密钥和自定义 Key。
//
// 使用指针字段（*string / *bool / *ObjectStorage）是为了区分「未在该文件里配置」
// 与「在管理后台里显式清空/关闭」——这样即使只读的 /etc/musicd/config.json 里留着旧
// Cookie，管理员在后台退出登录后重启也不会被旧配置复活。
type Secrets struct {
	Version  int               `json:"version"`
	Auth     AuthSecrets       `json:"auth,omitempty"`
	Netease  NeteaseSecrets    `json:"netease,omitempty"`
	Bilibili BilibiliSecrets   `json:"bilibili,omitempty"`
	Storage  *ObjectStorage    `json:"storage,omitempty"`
	Keys     map[string]string `json:"keys,omitempty"`
}

// AuthSecrets 管理服务器级超管与房间管理员口令。
type AuthSecrets struct {
	RootPassword  string `json:"rootPassword,omitempty"`
	AdminPassword string `json:"adminPassword,omitempty"`
}

// NeteaseSecrets 管理网易云显式 Cookie。
type NeteaseSecrets struct {
	Cookie *string `json:"cookie,omitempty"`
}

// BilibiliSecrets 管理 B 站开关、登录态策略与显式 Cookie。
type BilibiliSecrets struct {
	Enabled  *bool   `json:"enabled,omitempty"`
	UseLogin *bool   `json:"useLogin,omitempty"`
	Cookie   *string `json:"cookie,omitempty"`
}

// Clone 返回 Secrets 的深拷贝。
func (s Secrets) Clone() Secrets {
	cp := Secrets{
		Version: s.Version,
		Auth:    s.Auth,
	}
	if s.Netease.Cookie != nil {
		v := *s.Netease.Cookie
		cp.Netease.Cookie = &v
	}
	if s.Bilibili.Enabled != nil {
		v := *s.Bilibili.Enabled
		cp.Bilibili.Enabled = &v
	}
	if s.Bilibili.UseLogin != nil {
		v := *s.Bilibili.UseLogin
		cp.Bilibili.UseLogin = &v
	}
	if s.Bilibili.Cookie != nil {
		v := *s.Bilibili.Cookie
		cp.Bilibili.Cookie = &v
	}
	if s.Storage != nil {
		st := *s.Storage
		st.Accounts = append([]StorageAccount(nil), s.Storage.Accounts...)
		cp.Storage = &st
	}
	if len(s.Keys) > 0 {
		cp.Keys = make(map[string]string, len(s.Keys))
		for k, v := range s.Keys {
			cp.Keys[k] = v
		}
	}
	return cp
}

// ResolveSecretsPath 计算密钥配置文件的最终路径。
//
// 优先级：
//  1. 环境变量 MUSIC_SECRETS_FILE
//  2. 主配置中的 secretsFile 字段
//  3. dataDir 下的 secrets.json（生产 systemd 下 /var/lib/musicd 可写，/etc/musicd 只读）
//  4. 本地开发路径：仓库根存在 server/ 时用 server/secrets.local.json，否则 secrets.local.json
func ResolveSecretsPath(cfg Config) string {
	if env := strings.TrimSpace(os.Getenv("MUSIC_SECRETS_FILE")); env != "" {
		return filepath.Clean(env)
	}
	if p := strings.TrimSpace(cfg.SecretsFile); p != "" {
		return filepath.Clean(p)
	}
	if dir := strings.TrimSpace(cfg.DataDir); dir != "" {
		return filepath.Join(filepath.Clean(dir), "secrets.json")
	}
	if st, err := os.Stat("server"); err == nil && st.IsDir() {
		return filepath.Join("server", "secrets.local.json")
	}
	return "secrets.local.json"
}

// LoadSecrets 读取密钥配置文件；文件不存在时返回空 Secrets 与 exists=false（不报错）。
func LoadSecrets(path string) (Secrets, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Secrets{Version: 1}, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Secrets{Version: 1}, false, nil
		}
		return Secrets{}, false, fmt.Errorf("读取密钥配置 %s: %w", path, err)
	}
	var s Secrets
	if err := json.Unmarshal(data, &s); err != nil {
		return Secrets{}, true, fmt.Errorf("解析密钥配置 %s: %w", path, err)
	}
	if s.Version <= 0 {
		s.Version = 1
	}
	return s, true, nil
}

// SaveSecrets 以 0600 权限原子落盘密钥配置文件。
func SaveSecrets(path string, s Secrets) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("密钥配置文件路径为空")
	}
	if s.Version <= 0 {
		s.Version = 1
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建密钥目录 %s: %w", dir, err)
		}
	}
	blob, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化密钥配置失败: %w", err)
	}
	blob = append(blob, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("写入临时密钥配置 %s: %w", tmp, err)
	}
	_ = os.Chmod(tmp, 0o600)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换密钥配置 %s: %w", path, err)
	}
	return nil
}

// ApplySecrets 将独立密钥配置合并到 Config 上。
func ApplySecrets(cfg *Config, s Secrets) {
	if v := strings.TrimSpace(s.Auth.RootPassword); v != "" {
		cfg.RootPassword = v
	}
	if v := strings.TrimSpace(s.Auth.AdminPassword); v != "" {
		cfg.AdminPassword = v
	}
	if s.Netease.Cookie != nil {
		cfg.NeteaseCookie = SanitizeCookie(*s.Netease.Cookie)
	}
	if s.Bilibili.Enabled != nil {
		cfg.Bilibili.Enabled = *s.Bilibili.Enabled
	}
	if s.Bilibili.UseLogin != nil {
		cfg.Bilibili.UseLogin = *s.Bilibili.UseLogin
	}
	if s.Bilibili.Cookie != nil {
		clean := SanitizeCookie(*s.Bilibili.Cookie)
		cfg.Bilibili.Cookie = clean
		cfg.BilibiliCookie = clean
	}
	if s.Storage != nil {
		st := *s.Storage
		st.Accounts = append([]StorageAccount(nil), s.Storage.Accounts...)
		cfg.Storage = st
	}
	if s.Keys != nil {
		if cfg.Keys == nil {
			cfg.Keys = make(map[string]string, len(s.Keys))
		}
		for k, v := range s.Keys {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			cfg.Keys[k] = v
		}
	}
}

// CheckWritable 探测目标密钥文件（或其父目录）是否可写，用于管理页展示只读提示。
func CheckWritable(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err == nil {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return false
		}
		_ = f.Close()
		return true
	}
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".musicd-writecheck-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// SanitizeCookie 去除换行与首尾空白，避免非法 HTTP 头。
func SanitizeCookie(raw string) string {
	raw = strings.ReplaceAll(raw, "\r", "")
	raw = strings.ReplaceAll(raw, "\n", "")
	return strings.TrimSpace(raw)
}

// CookieKeys 提取 Cookie 字符串里出现的非空键名（升序去重），绝不暴露值。
func CookieKeys(raw string) []string {
	seen := make(map[string]struct{})
	for _, part := range strings.Split(SanitizeCookie(raw), ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !ok || k == "" || v == "" {
			continue
		}
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MaskSecret 对敏感字符串做脱敏展示，不暴露完整凭据。
func MaskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	runes := []rune(s)
	n := len(runes)
	if n <= 4 {
		return strings.Repeat("*", n)
	}
	if n <= 10 {
		return string(runes[:2]) + strings.Repeat("*", n-4) + string(runes[n-2:])
	}
	return string(runes[:4]) + "…" + string(runes[n-4:])
}

// BoolPtr 返回 bool 指针。
func BoolPtr(v bool) *bool { return &v }

// StrPtr 返回 string 指针。
func StrPtr(v string) *string { return &v }
