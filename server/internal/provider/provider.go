// Package provider 定义音乐源接口与注册表。
//
// v1 只有两个实现：netease（source 前缀 "wy"）与 demo（source 前缀 "demo"）。
package provider

import (
	"context"
	"strings"

	"github.com/hakrelin/music/server/internal/model"
)

// Provider 是一个音乐源。
type Provider interface {
	// Name 返回 provider 名称，如 "wy"、"demo"。
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

// Registry 按 source 路由到 Provider。
type Registry struct {
	byName map[string]Provider
}

func NewRegistry() *Registry { return &Registry{byName: map[string]Provider{}} }

func (r *Registry) Register(p Provider) { r.byName[p.Name()] = p }

// For 返回 source 对应的 provider；source 为空时返回默认 provider（demo）。
func (r *Registry) For(source string) (Provider, bool) {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "demo"
	}
	p, ok := r.byName[source]
	return p, ok
}

// ForTrack 与 For 相同，但会忽略非法 source。
func (r *Registry) ForTrack(t model.Track) (Provider, bool) { return r.For(t.Source) }

// Names 返回已注册的 source 列表（用于 /api/config）。
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for k := range r.byName {
		out = append(out, k)
	}
	return out
}
