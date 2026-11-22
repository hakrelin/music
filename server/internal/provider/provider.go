// Package provider 定义音乐源接口与注册表。
//
// 当前支持：netease（source = "wy"）、bilibili（source = "bili"）与 demo（source = "demo"）。
package provider

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/hakrelin/music/server/internal/model"
)

// Provider 是一个音乐源。
type Provider interface {
	// Name 返回 provider 名称，如 "wy"、"bili"、"demo"。
	Name() string
	// Search 按关键词搜歌曲。返回曲目、总数。
	Search(ctx context.Context, q string, page, size int) ([]model.Track, int, error)
	// Playlist 取歌单详情与曲目。
	Playlist(ctx context.Context, id string) (model.Playlist, []model.Track, error)
	// UserPlaylists 取某用户创建的公开歌单。
	UserPlaylists(ctx context.Context, uid string) ([]model.Playlist, error)
	// Resolve 填 Track.URL（必要时补 cover 等）。失败不致命，调用方跳过该曲。
	Resolve(ctx context.Context, t *model.Track) error
	// Lyric 取 LRC 歌词原文（可能为空）。
	Lyric(ctx context.Context, id string) (string, error)
}

// LyricResult 是一首歌的歌词原文与翻译（翻译可能为空串）。
type LyricResult struct {
	Lrc   string
	Trans string
}

// LyricProvider 是可选能力：能同时返回翻译的 provider 实现它，
// hub 会优先用 LyricFull 来生成双语歌词。
type LyricProvider interface {
	LyricFull(ctx context.Context, id string) (LyricResult, error)
}

// DetailProvider 是可选能力：按 ID 补齐单曲元数据（时长/封面/fee）。
// hub 在用户点歌时用它兜底，保证队列里的时长不是 0。
type DetailProvider interface {
	TrackDetail(ctx context.Context, id string) (model.Track, error)
}

// Registry 按 source 路由到 Provider（并发安全，支持管理后台热更新）。
type Registry struct {
	mu     sync.RWMutex
	byName map[string]Provider
}

func NewRegistry() *Registry { return &Registry{byName: map[string]Provider{}} }

func (r *Registry) Register(p Provider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	r.byName[p.Name()] = p
	r.mu.Unlock()
}

func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	delete(r.byName, strings.TrimSpace(name))
	r.mu.Unlock()
}

// For 返回 source 对应的 provider；source 为空时返回默认 provider（demo）。
func (r *Registry) For(source string) (Provider, bool) {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "demo"
	}
	r.mu.RLock()
	p, ok := r.byName[source]
	r.mu.RUnlock()
	return p, ok
}

// ForTrack 与 For 相同，但会忽略非法 source。
func (r *Registry) ForTrack(t model.Track) (Provider, bool) { return r.For(t.Source) }

// Names 返回已注册的 source 列表（用于 /api/config）。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byName))
	for k := range r.byName {
		out = append(out, k)
	}
	return out
}

// UserSearcher is optional: nickname search returns distinct accounts to choose from.
type UserSearcher interface {
	SearchUsers(context.Context, string, int, int) ([]model.User, int, error)
}

// RefreshProvider can lower quality when the client cannot play the original stream.
type RefreshProvider interface {
	ResolveFresh(context.Context, *model.Track, bool) error
}

// StreamProxyProvider 是可选能力：对于上游 CDN 强校验 Referer（如 B 站）的音源，
// /api/stream/{id} 在对象存储未命中时会交由该接口带 Referer + Range 透传纯音频流。
type StreamProxyProvider interface {
	ServeAudio(w http.ResponseWriter, r *http.Request, id string, lower bool) bool
}

// UpstreamAudioResolver 是可选能力：解析真实上游音频地址（供对象存储缓存预热下载用）。
type UpstreamAudioResolver interface {
	ResolveUpstreamAudio(ctx context.Context, id string, lower bool) (string, error)
}
