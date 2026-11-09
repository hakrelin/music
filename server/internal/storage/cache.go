package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// 多吉云「简单上传」接口上限 10MB，留 0.5MB 余量。
	maxUploadBytes = 9_500_000
	// 浏览器拿到的预签名地址有效期：比一首歌长得多，够它拖完全程。
	presignTTL = 2 * time.Hour
	// 临时密钥提前多久刷新。
	credsRefreshWindow = 10 * time.Minute
	// 索引落盘防抖。
	saveDebounce = 3 * time.Second
)

// DownloadFunc 把播放地址整段拉下来；truncated 表示超过 limit。
type DownloadFunc func(ctx context.Context, rawURL string, limit int64) (data []byte, truncated bool, err error)

// Resolver 解析一首歌的播放地址（缓存预热用 128k 版本，体积才压得进 10MB）。
type Resolver func(ctx context.Context, source, id string) (string, error)

// Options 构造 Cache。
type Options struct {
	Accounts           []Account
	DataDir            string
	MaxBytesPerAccount int64 // 0 表示不限
	EvictAfterDays     int   // 0 表示不按时间淘汰
	MinHits            int   // 时间淘汰时，命中次数达到这个值就保留
	Download           DownloadFunc
	Logger             *slog.Logger
}

// Item 是缓存索引里的一首歌。
type Item struct {
	Account int    `json:"account"`
	Key     string `json:"key"`
	Size    int64  `json:"size"`
	Hits    int    `json:"hits"`
	Created int64  `json:"created"`
	Access  int64  `json:"access"`
}

// Cache 管理「曲目 → 对象存储」的映射、上传与淘汰。
type Cache struct {
	opts Options
	log  *slog.Logger
	hc   *http.Client

	accounts []*account

	// fillSem 限制同时进行的预热数量。
	// 服务器出网只有 3Mbps，一条 128k 的歌约 4MB，传一次要占满带宽十几秒；
	// 并发预热带宽会互相拖，页面首屏也会变慢，所以串行做。
	fillSem chan struct{}

	mu       sync.Mutex
	items    map[string]*Item
	inflight map[string]bool

	dirty   bool
	saveSig chan struct{}
}

type account struct {
	Account
	cl *client

	mu    sync.Mutex
	creds *Credentials
	exp   time.Time
	hosts map[string]string // 桶名 → COS vhost
}

// New 构造缓存；accounts 全为空时返回 nil（调用方据此关闭缓存功能）。
func New(opts Options) *Cache {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Download == nil {
		opts.Download = func(context.Context, string, int64) ([]byte, bool, error) {
			return nil, false, fmt.Errorf("未配置下载实现")
		}
	}
	if opts.MinHits <= 0 {
		opts.MinHits = 2
	}
	c := &Cache{
		opts:     opts,
		log:      opts.Logger,
		hc:       &http.Client{Timeout: 60 * time.Second},
		fillSem:  make(chan struct{}, 1),
		items:    make(map[string]*Item),
		inflight: make(map[string]bool),
		saveSig:  make(chan struct{}, 1),
	}
	for _, acc := range opts.Accounts {
		if acc.AK == "" || acc.SK == "" || acc.Bucket == "" {
			continue
		}
		if acc.Label == "" {
			acc.Label = acc.Bucket
		}
		c.accounts = append(c.accounts, &account{
			Account: acc,
			cl:      newClient(acc, c.hc),
			hosts:   make(map[string]string),
		})
	}
	if len(c.accounts) == 0 {
		return nil
	}
	c.load()
	go c.saver()
	return c
}

// Enabled 返回缓存是否可用。
func (c *Cache) Enabled() bool { return c != nil && len(c.accounts) > 0 }

// keyFor 生成对象 key。用「源/曲目 ID」而不是曲名，避免中文与特殊字符问题。
func keyFor(source, id string) string {
	if source == "" {
		source = "wy"
	}
	return "audio/" + source + "/" + id + ".mp3"
}

func itemKey(source, id string) string { return source + ":" + id }

// accountFor 按 key 哈希在账号之间交替存放（两个账号分摊流量与容量）。
func (c *Cache) accountFor(key string) int {
	if len(c.accounts) == 1 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(len(c.accounts)))
}

// Lookup 命中缓存时返回可直接播放的预签名地址。
func (c *Cache) Lookup(source, id string) (string, bool) {
	if !c.Enabled() {
		return "", false
	}
	ik := itemKey(source, id)
	c.mu.Lock()
	it, ok := c.items[ik]
	if ok {
		it.Hits++
		it.Access = time.Now().Unix()
		c.markDirtyLocked()
	}
	var idx int
	var objKey string
	if ok {
		idx, objKey = it.Account, it.Key
	}
	c.mu.Unlock()
	if !ok {
		return "", false
	}
	if idx < 0 || idx >= len(c.accounts) {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	u, err := c.accounts[idx].presign(ctx, objKey)
	if err != nil {
		c.log.Warn("缓存命中但签名失败", "key", objKey, "err", err)
		return "", false
	}
	return u, true
}

// Has 判断是否已缓存（不增加命中计数）。
func (c *Cache) Has(source, id string) bool {
	if !c.Enabled() {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.items[itemKey(source, id)]
	return ok
}

// Ensure 在后台把一首歌补进对象存储；已缓存或正在补时立即返回。
//
// 这是「听过的歌缓存起来，下次不走网易云」的入口：房间开始播某首歌、
// 或者前端回落到 /api/stream 时都会调它。
func (c *Cache) Ensure(source, id string, resolve Resolver) {
	if !c.Enabled() || id == "" || resolve == nil {
		return
	}
	ik := itemKey(source, id)
	c.mu.Lock()
	if _, ok := c.items[ik]; ok {
		c.mu.Unlock()
		return
	}
	if c.inflight[ik] {
		c.mu.Unlock()
		return
	}
	c.inflight[ik] = true
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.inflight, ik)
			c.mu.Unlock()
		}()
		select {
		case c.fillSem <- struct{}{}:
		case <-time.After(2 * time.Minute):
			return
		}
		defer func() { <-c.fillSem }()
		c.fill(source, id, resolve)
	}()
}

// fill 是一次完整的预热：解析 128k 地址 → 下载 → 上传到对象存储 → 记索引。
func (c *Cache) fill(source, id string, resolve Resolver) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	url, err := resolve(ctx, source, id)
	if err != nil || url == "" {
		c.log.Warn("缓存预热：解析播放地址失败", "track", id, "err", err)
		return
	}
	data, truncated, err := c.opts.Download(ctx, url, maxUploadBytes)
	if err != nil {
		c.log.Warn("缓存预热：下载失败", "track", id, "err", err)
		return
	}
	if truncated {
		c.log.Info("缓存预热：文件超过 10MB，跳过缓存", "track", id)
		return
	}
	if len(data) == 0 {
		c.log.Warn("缓存预热：下载到 0 字节", "track", id)
		return
	}

	key := keyFor(source, id)
	idx := c.accountFor(key)
	// 主账号失败就换另一个账号再试一次（两个账号交替兜底）。
	var lastErr error
	for attempt := 0; attempt < len(c.accounts); attempt++ {
		acc := c.accounts[(idx+attempt)%len(c.accounts)]
		if err := acc.cl.Upload(ctx, acc.Bucket, key, data, contentTypeFor(key)); err != nil {
			lastErr = err
			c.log.Warn("缓存预热：上传失败", "track", id, "bucket", acc.Bucket, "err", err)
			continue
		}
		now := time.Now().Unix()
		c.mu.Lock()
		c.items[itemKey(source, id)] = &Item{
			Account: (idx + attempt) % len(c.accounts),
			Key:     key,
			Size:    int64(len(data)),
			Created: now,
			Access:  now,
		}
		c.markDirtyLocked()
		c.mu.Unlock()
		c.log.Info("缓存预热完成",
			"track", id, "bucket", acc.Bucket, "bytes", len(data),
			"elapsedMs", time.Since(started).Milliseconds())
		return
	}
	if lastErr != nil {
		c.log.Warn("缓存预热：所有账号都上传失败", "track", id, "err", lastErr)
	}
}

// presign 生成某个对象的预签名地址（临时密钥自动续期）。
func (a *account) presign(ctx context.Context, key string) (string, error) {
	// 配了加速域名（基础型存储空间必须这样）：直接用 "域名 + key" 播放，
	// 既不需要 S3 临时密钥，也不产生 0.5 元/GB 的 SDK 流出流量。
	if base := strings.TrimSuffix(strings.TrimSpace(a.Base), "/"); base != "" {
		return base + "/" + strings.TrimPrefix(key, "/"), nil
	}
	a.mu.Lock()
	need := a.creds == nil || time.Until(a.exp) < credsRefreshWindow
	a.mu.Unlock()
	if need {
		resp, err := a.cl.tempCredentials(ctx)
		if err != nil {
			return "", err
		}
		host := ""
		for _, b := range resp.Buckets {
			if b.S3EndpointHost != "" {
				host = b.S3EndpointHost
			}
			if b.Name == a.Bucket && b.S3EndpointHost != "" {
				host = b.S3EndpointHost
				break
			}
		}
		if host == "" {
			return "", fmt.Errorf("临时密钥里没有 %s 的访问域名", a.Bucket)
		}
		a.mu.Lock()
		a.creds = &resp.Credentials
		a.exp = time.Unix(resp.ExpiredAt, 0)
		a.hosts[a.Bucket] = host
		a.mu.Unlock()
	}
	a.mu.Lock()
	creds := *a.creds
	host := a.hosts[a.Bucket]
	a.mu.Unlock()
	return presignGet(host, key, creds, presignTTL)
}

// ---------- 索引持久化 ----------

type indexFile struct {
	Version int              `json:"version"`
	SavedAt int64            `json:"savedAt"`
	Items   map[string]*Item `json:"items"`
}

func (c *Cache) indexPath() string {
	if c.opts.DataDir == "" {
		return ""
	}
	return filepath.Join(c.opts.DataDir, "audio-cache.json")
}

func (c *Cache) load() {
	p := c.indexPath()
	if p == "" {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			c.log.Warn("读取缓存索引失败", "path", p, "err", err)
		}
		return
	}
	var f indexFile
	if err := json.Unmarshal(data, &f); err != nil {
		c.log.Warn("缓存索引损坏，忽略", "path", p, "err", err)
		return
	}
	c.mu.Lock()
	for k, v := range f.Items {
		if v == nil || v.Account < 0 || v.Account >= len(c.accounts) {
			continue
		}
		c.items[k] = v
	}
	n := len(c.items)
	c.mu.Unlock()
	c.log.Info("缓存索引已加载", "path", p, "items", n)
}

func (c *Cache) markDirtyLocked() {
	c.dirty = true
	select {
	case c.saveSig <- struct{}{}:
	default:
	}
}

// saver 把索引防抖落盘：3 秒内多次变更只写一次。
func (c *Cache) saver() {
	for range c.saveSig {
		time.Sleep(saveDebounce)
		c.mu.Lock()
		if !c.dirty {
			c.mu.Unlock()
			continue
		}
		snapshot := make(map[string]*Item, len(c.items))
		for k, v := range c.items {
			cp := *v
			snapshot[k] = &cp
		}
		c.dirty = false
		c.mu.Unlock()
		c.writeIndex(snapshot)
	}
}

func (c *Cache) writeIndex(items map[string]*Item) {
	p := c.indexPath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		c.log.Warn("创建数据目录失败", "dir", filepath.Dir(p), "err", err)
		return
	}
	blob, err := json.Marshal(indexFile{Version: 1, SavedAt: time.Now().Unix(), Items: items})
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		c.log.Warn("写缓存索引失败", "path", p, "err", err)
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		c.log.Warn("替换缓存索引失败", "path", p, "err", err)
	}
}

// ---------- 淘汰 ----------

// Run 起一个每日巡检：按时间和容量淘汰旧对象，避免桶无限增长。
func (c *Cache) Run(ctx context.Context) {
	if !c.Enabled() {
		return
	}
	// 启动后先跑一次，之后每天一次（月初清量最大，和「每月淘汰一部分」一致）。
	go func() {
		c.Evict(ctx)
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.Evict(ctx)
			}
		}
	}()
}

// Evict 按「时间 + 容量」淘汰缓存对象。
func (c *Cache) Evict(ctx context.Context) {
	if !c.Enabled() {
		return
	}
	now := time.Now()
	type cand struct {
		ik string
		it Item
	}
	c.mu.Lock()
	var expired []cand
	total := make([]int64, len(c.accounts))
	for ik, it := range c.items {
		if it.Account >= 0 && it.Account < len(total) {
			total[it.Account] += it.Size
		}
		if c.opts.EvictAfterDays > 0 {
			age := now.Sub(time.Unix(it.Access, 0))
			if age > time.Duration(c.opts.EvictAfterDays)*24*time.Hour && it.Hits < c.opts.MinHits {
				expired = append(expired, cand{ik: ik, it: *it})
			}
		}
	}
	c.mu.Unlock()

	deleted := 0
	for _, e := range expired {
		if c.deleteItem(ctx, e.ik, e.it) {
			deleted++
		}
	}

	// 容量淘汰：某个账号超额时，按 (命中次数升序, 最近访问升序) 删到 90%。
	if c.opts.MaxBytesPerAccount > 0 {
		for idx := range c.accounts {
			c.mu.Lock()
			var list []cand
			var sum int64
			for ik, it := range c.items {
				if it.Account != idx {
					continue
				}
				list = append(list, cand{ik: ik, it: *it})
				sum += it.Size
			}
			c.mu.Unlock()
			if sum <= c.opts.MaxBytesPerAccount {
				continue
			}
			sort.Slice(list, func(i, j int) bool {
				if list[i].it.Hits != list[j].it.Hits {
					return list[i].it.Hits < list[j].it.Hits
				}
				return list[i].it.Access < list[j].it.Access
			})
			target := c.opts.MaxBytesPerAccount * 9 / 10
			for _, e := range list {
				if sum <= target {
					break
				}
				if c.deleteItem(ctx, e.ik, e.it) {
					sum -= e.it.Size
					deleted++
				}
			}
		}
	}
	if deleted > 0 {
		c.log.Info("缓存淘汰完成", "deleted", deleted, "remaining", c.Count())
	}
}

func (c *Cache) deleteItem(ctx context.Context, ik string, it Item) bool {
	if it.Account < 0 || it.Account >= len(c.accounts) {
		return false
	}
	acc := c.accounts[it.Account]
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := acc.cl.DeleteKeys(dctx, acc.Bucket, []string{it.Key}); err != nil {
		c.log.Warn("删除缓存对象失败", "key", it.Key, "bucket", acc.Bucket, "err", err)
		return false
	}
	c.mu.Lock()
	delete(c.items, ik)
	c.markDirtyLocked()
	c.mu.Unlock()
	return true
}

// Count 返回索引里的歌曲数。
func (c *Cache) Count() int {
	if !c.Enabled() {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Stats 给 /api/cache/stats 用。
func (c *Cache) Stats() map[string]any {
	out := map[string]any{"enabled": c.Enabled()}
	if !c.Enabled() {
		return out
	}
	c.mu.Lock()
	per := make([]map[string]any, len(c.accounts))
	for i, a := range c.accounts {
		per[i] = map[string]any{"label": a.Label, "bucket": a.Bucket, "items": 0, "bytes": int64(0)}
	}
	hits := 0
	for _, it := range c.items {
		if it.Account >= 0 && it.Account < len(per) {
			per[it.Account]["items"] = per[it.Account]["items"].(int) + 1
			per[it.Account]["bytes"] = per[it.Account]["bytes"].(int64) + it.Size
		}
		hits += it.Hits
	}
	out["items"] = len(c.items)
	out["hits"] = hits
	out["accounts"] = per
	out["inflight"] = len(c.inflight)
	c.mu.Unlock()
	return out
}
