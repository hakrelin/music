// Package config 负责服务端配置的加载。
//
// 优先级（后者覆盖前者）：
//  1. 内置默认值（保证不带任何配置文件也能跑起来）
//  2. 示例配置 config.example.json（提交进仓库）
//  3. 本地配置 config.local.json / /etc/musicd/config.json / MUSIC_CONFIG
//  4. 独立密钥配置 secrets.json（管理 Cookie / Key / 口令 / 对象存储密钥，支持后台热更新落盘）
//  5. 环境变量（MUSIC_*）
//
// 之所以一层层覆盖而不是"只读一个文件"，是因为部署时主配置真源在只读的
// /etc/musicd/config.json（见 docs/architecture.md §9），而运行时后台登录/配置
// 写进可写的 secrets.json（默认 <dataDir>/secrets.json），两种场景都能无缝兼容。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Site 是站点展示信息（/api/config 里给前端）。
type Site struct {
	Name    string `json:"name"`
	Tagline string `json:"tagline"`
}

// RadioRoom 是一个内置电台房的声明。
type RadioRoom struct {
	Name      string   `json:"name"`
	Desc      string   `json:"desc"`
	Playlists []string `json:"playlists"`
}

// BilibiliConfig 是 B 站搜歌与抽音的配置。
//
// 默认启用且默认使用登录态（UseLogin=true），Cookie 需显式配置在 secrets.json /
// config.local.json / MUSIC_BILIBILI_COOKIE 或在管理后台扫码登录获得。
type BilibiliConfig struct {
	Enabled  bool   `json:"enabled"`
	UseLogin bool   `json:"useLogin"`
	Cookie   string `json:"cookie"`
}

// Config 是完整服务端配置，JSON 字段名与 docs/architecture.md §6 一致。
type Config struct {
	HTTPAddr        string            `json:"httpAddr"`
	Provider        string            `json:"provider"` // auto | wy | demo
	SecretsFile     string            `json:"secretsFile,omitempty"`
	NeteaseCookie   string            `json:"neteaseCookie"`
	BilibiliCookie  string            `json:"bilibiliCookie,omitempty"` // 平铺兼容写法
	Bilibili        BilibiliConfig    `json:"bilibili"`
	Site            Site              `json:"site"`
	RootPassword    string            `json:"rootPassword"`
	AdminPassword   string            `json:"adminPassword"`
	RadioRooms      []RadioRoom       `json:"radioRooms"`
	RoomIdleMinutes int               `json:"roomIdleMinutes"` // <=0 表示不回收
	VoteSkipRatio   float64           `json:"voteSkipRatio"`   // (0,1]
	LogLevel        string            `json:"logLevel"`
	DataDir         string            `json:"dataDir"` // 持久化目录（缓存索引与默认 secrets.json 也放这里）
	Storage         ObjectStorage     `json:"storage"`
	Keys            map[string]string `json:"keys,omitempty"`
}

// Meta 记录本次加载的来源信息，便于管理后台展示与持久化。
type Meta struct {
	ConfigFiles   []string
	SecretsPath   string
	SecretsExists bool
	Secrets       Secrets
	EnvOverrides  []string
}

// StorageAccount 是对象存储的一个账号。
//
// 目前接的是多吉云（底层腾讯 COS）：上传/删除走多吉云服务端 API，
// 播放走 302 预签名地址，浏览器直连 COS，不吃服务器出网带宽。
type StorageAccount struct {
	Label     string `json:"label"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	Bucket    string `json:"bucket"`
	// PublicBase 是这个桶绑定的加速域名（如 https://audio.tea-kompeito.com）。
	//
	// 基础型存储空间不支持 S3 临时密钥（没法签预签名 URL），只能靠绑定的
	// CDN 域名对外提供文件；配了它就用 "域名 + key" 直接播放，没配就退回
	// 标准型的预签名方式。
	PublicBase string `json:"publicBase"`
}

// ObjectStorage 是音频缓存配置。accounts 为空时整个功能关闭。
type ObjectStorage struct {
	Enabled         bool             `json:"enabled"`
	Accounts        []StorageAccount `json:"accounts"`
	MaxMBPerAccount int              `json:"maxMBPerAccount"` // 0 = 不限容量
	EvictAfterDays  int              `json:"evictAfterDays"`  // 0 = 不按时间淘汰
	MinHits         int              `json:"minHits"`         // 时间淘汰时的保护阈值
}

// Clone 返回 Config 的深拷贝。
func (c Config) Clone() Config {
	cp := c
	cp.RadioRooms = make([]RadioRoom, len(c.RadioRooms))
	for i, r := range c.RadioRooms {
		r.Playlists = append([]string(nil), r.Playlists...)
		cp.RadioRooms[i] = r
	}
	cp.Storage.Accounts = append([]StorageAccount(nil), c.Storage.Accounts...)
	if len(c.Keys) > 0 {
		cp.Keys = make(map[string]string, len(c.Keys))
		for k, v := range c.Keys {
			cp.Keys[k] = v
		}
	} else {
		cp.Keys = nil
	}
	return cp
}

// Default 返回内置默认配置（与 config.example.json 保持一致）。
func Default() Config {
	return Config{
		HTTPAddr:      ":8080",
		Provider:      "auto",
		NeteaseCookie: "",
		Bilibili: BilibiliConfig{
			Enabled:  true,
			UseLogin: true,
			Cookie:   "",
		},
		Site: Site{
			Name:    "茶与金平糖",
			Tagline: "和大家一起听歌",
		},
		RootPassword:  "654321",
		AdminPassword: "123456",
		RadioRooms: []RadioRoom{{
			Name: "东方电台",
			Desc: "24 小时东方 Project，不间断",
			Playlists: []string{
				"2416955681", "406607901", "977204775",
				"434256234", "829520066", "2824632945",
			},
		}},
		RoomIdleMinutes: 10,
		VoteSkipRatio:   0.3,
		LogLevel:        "info",
	}
}

// exampleCandidates/localCandidates 是相对工作目录的候选路径。
//
// 本仓库约定从仓库根目录启动（`go run ./server/cmd/musicd`），所以同时
// 兼容两种写法：带 server/ 前缀（仓库根）和不带（已 cd 进 server/）。
// 最后一项是部署路径，见 docs §9。
var (
	exampleCandidates = []string{
		"server/config.example.json",
		"config.example.json",
		"/etc/musicd/config.example.json",
	}
	localCandidates = []string{
		"server/config.local.json",
		"config.local.json",
		"/etc/musicd/config.json",
	}
)

// Load 按默认值 → example → local → secrets → env 的顺序加载配置。
func Load() (Config, error) {
	cfg, _, err := LoadWithMeta("")
	return cfg, err
}

// LoadFrom 与 Load 相同，但允许显式指定一个额外文件（通常是 MUSIC_CONFIG），
// 它在 local 之后、secrets/env 之前生效。file 为空时行为与 Load 一致。
func LoadFrom(file string) (Config, error) {
	cfg, _, err := LoadWithMeta(file)
	return cfg, err
}

// LoadWithMeta 加载配置并返回来源元信息（含解析出的密钥配置路径与内容）。
func LoadWithMeta(file string) (Config, Meta, error) {
	cfg := Default()
	meta := Meta{Secrets: Secrets{Version: 1}}

	if p, err := firstExisting(exampleCandidates); err != nil {
		return cfg, meta, err
	} else if p != "" {
		if err := mergeFile(&cfg, p); err != nil {
			return cfg, meta, err
		}
		meta.ConfigFiles = append(meta.ConfigFiles, p)
	}

	if p, err := firstExisting(localCandidates); err != nil {
		return cfg, meta, err
	} else if p != "" {
		if err := mergeFile(&cfg, p); err != nil {
			return cfg, meta, err
		}
		meta.ConfigFiles = append(meta.ConfigFiles, p)
	}

	if file == "" {
		file = strings.TrimSpace(os.Getenv("MUSIC_CONFIG"))
	}
	if file != "" {
		if err := mergeFile(&cfg, file); err != nil {
			return cfg, meta, err
		}
		meta.ConfigFiles = append(meta.ConfigFiles, file)
	}

	// 允许用 MUSIC_DATA_DIR / MUSIC_SECRETS_FILE 影响 secrets.json 的定位。
	if v, ok := os.LookupEnv("MUSIC_DATA_DIR"); ok && strings.TrimSpace(v) != "" {
		cfg.DataDir = v
	}
	if v, ok := os.LookupEnv("MUSIC_SECRETS_FILE"); ok && strings.TrimSpace(v) != "" {
		cfg.SecretsFile = v
	}

	meta.SecretsPath = ResolveSecretsPath(cfg)
	cfg.SecretsFile = meta.SecretsPath

	sec, exists, err := LoadSecrets(meta.SecretsPath)
	if err != nil {
		return cfg, meta, err
	}
	meta.SecretsExists = exists
	meta.Secrets = sec
	if exists {
		ApplySecrets(&cfg, sec)
	}

	meta.EnvOverrides = applyEnv(&cfg)
	cfg.Normalize()
	return cfg, meta, nil
}

// firstExisting 返回第一个存在的候选文件；都不存在时返回空串（不报错）。
func firstExisting(candidates []string) (string, error) {
	for _, p := range candidates {
		st, err := os.Stat(p)
		if err == nil {
			if st.IsDir() {
				continue
			}
			return p, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", nil
}

// mergeFile 把 JSON 文件合并进 cfg：文件里没写的字段保留原值。
func mergeFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置文件 %s: %w", path, err)
	}
	// 允许 JSON 里写注释（jsonc 风格）会让实现变复杂，这里只接受严格 JSON。
	if err := json.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("解析配置文件 %s: %w", path, err)
	}
	return nil
}

// applyEnv 用环境变量覆盖配置，并返回实际生效的环境变量名列表。
func applyEnv(cfg *Config) []string {
	var overrides []string
	setStr := func(key string, dst *string) {
		if v, ok := os.LookupEnv(key); ok {
			*dst = v
			overrides = append(overrides, key)
		}
	}
	setInt := func(key string, dst *int) {
		if v, ok := os.LookupEnv(key); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				*dst = n
				overrides = append(overrides, key)
			}
		}
	}
	setFloat := func(key string, dst *float64) {
		if v, ok := os.LookupEnv(key); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				*dst = f
				overrides = append(overrides, key)
			}
		}
	}
	setBool := func(key string, dst *bool) {
		if v, ok := os.LookupEnv(key); ok {
			if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
				*dst = b
				overrides = append(overrides, key)
			}
		}
	}

	setStr("MUSIC_HTTP_ADDR", &cfg.HTTPAddr)
	setStr("MUSIC_PROVIDER", &cfg.Provider)
	setStr("MUSIC_SECRETS_FILE", &cfg.SecretsFile)
	setStr("MUSIC_NETEASE_COOKIE", &cfg.NeteaseCookie)
	if v, ok := os.LookupEnv("MUSIC_BILIBILI_COOKIE"); ok {
		cfg.Bilibili.Cookie = v
		cfg.BilibiliCookie = v
		overrides = append(overrides, "MUSIC_BILIBILI_COOKIE")
	}
	setBool("MUSIC_BILIBILI_ENABLED", &cfg.Bilibili.Enabled)
	setBool("MUSIC_BILIBILI_USE_LOGIN", &cfg.Bilibili.UseLogin)
	setStr("MUSIC_DATA_DIR", &cfg.DataDir)
	setStr("MUSIC_ROOT_PASSWORD", &cfg.RootPassword)
	setStr("MUSIC_ADMIN_PASSWORD", &cfg.AdminPassword)
	setStr("MUSIC_LOG_LEVEL", &cfg.LogLevel)
	setStr("MUSIC_SITE_NAME", &cfg.Site.Name)
	setStr("MUSIC_SITE_TAGLINE", &cfg.Site.Tagline)
	setInt("MUSIC_ROOM_IDLE_MINUTES", &cfg.RoomIdleMinutes)
	setFloat("MUSIC_VOTE_SKIP_RATIO", &cfg.VoteSkipRatio)
	return overrides
}

// Normalize 兜底修正非法/缺失值，让后续代码可以放心使用。
func (c *Config) Normalize() {
	if strings.TrimSpace(c.HTTPAddr) == "" {
		c.HTTPAddr = ":8080"
	}
	c.Provider = strings.ToLower(strings.TrimSpace(c.Provider))
	switch c.Provider {
	case "auto", "wy", "demo":
	default:
		c.Provider = "auto"
	}
	c.NeteaseCookie = SanitizeCookie(c.NeteaseCookie)
	c.Bilibili.Cookie = SanitizeCookie(c.Bilibili.Cookie)
	c.BilibiliCookie = SanitizeCookie(c.BilibiliCookie)
	if c.Bilibili.Cookie == "" && c.BilibiliCookie != "" {
		c.Bilibili.Cookie = c.BilibiliCookie
	}
	c.BilibiliCookie = c.Bilibili.Cookie

	if c.Site.Name == "" {
		c.Site.Name = "茶与金平糖"
	}
	if c.Site.Tagline == "" {
		c.Site.Tagline = "和大家一起听歌"
	}
	// 0 也视为"关闭回收"，负数归一成 0。
	if c.RoomIdleMinutes < 0 {
		c.RoomIdleMinutes = 0
	}
	if c.VoteSkipRatio <= 0 || c.VoteSkipRatio > 1 {
		c.VoteSkipRatio = 0.3
	}
	switch strings.ToLower(strings.TrimSpace(c.LogLevel)) {
	case "debug", "info", "warn", "error":
		c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	default:
		c.LogLevel = "info"
	}
	if c.DataDir != "" {
		c.DataDir = filepath.Clean(c.DataDir)
	}
	if c.SecretsFile != "" {
		c.SecretsFile = filepath.Clean(c.SecretsFile)
	}
	// 对象存储：没配账号就整体关闭，避免半开状态下每次播放都白跑一次。
	accs := make([]StorageAccount, 0, len(c.Storage.Accounts))
	for _, a := range c.Storage.Accounts {
		a.AccessKey = strings.TrimSpace(a.AccessKey)
		a.SecretKey = strings.TrimSpace(a.SecretKey)
		a.Bucket = strings.TrimSpace(a.Bucket)
		a.Label = strings.TrimSpace(a.Label)
		a.PublicBase = strings.TrimSpace(a.PublicBase)
		if a.AccessKey == "" || a.SecretKey == "" || a.Bucket == "" {
			continue
		}
		accs = append(accs, a)
	}
	c.Storage.Accounts = accs
	if len(accs) == 0 {
		c.Storage.Enabled = false
	}
	if c.Storage.MaxMBPerAccount < 0 {
		c.Storage.MaxMBPerAccount = 0
	}
	if c.Storage.EvictAfterDays < 0 {
		c.Storage.EvictAfterDays = 0
	}
	if c.Storage.MinHits < 0 {
		c.Storage.MinHits = 0
	}
	if len(c.Keys) > 0 {
		cleaned := make(map[string]string, len(c.Keys))
		for k, v := range c.Keys {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			cleaned[k] = v
		}
		c.Keys = cleaned
	}
	// 电台房：丢掉没名字的条目，并保证 playlists 是独立切片。
	rooms := make([]RadioRoom, 0, len(c.RadioRooms))
	for _, r := range c.RadioRooms {
		r.Name = strings.TrimSpace(r.Name)
		if r.Name == "" {
			continue
		}
		if r.Desc == "" {
			r.Desc = "24 小时不间断电台"
		}
		pl := make([]string, 0, len(r.Playlists))
		for _, id := range r.Playlists {
			if id = strings.TrimSpace(id); id != "" {
				pl = append(pl, id)
			}
		}
		r.Playlists = pl
		rooms = append(rooms, r)
	}
	c.RadioRooms = rooms
}

// IdleTimeout 返回空房回收时长：0 表示关闭回收。
func (c Config) IdleTimeout() int {
	return c.RoomIdleMinutes
}
