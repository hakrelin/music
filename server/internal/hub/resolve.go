package hub

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider"
)

// providerTimeout 是单次 provider 调用的上限。
//
// 为什么是 8 秒：网易云接口正常 100–500ms，偶尔 2–3s；再久就是挂了。
// 房间状态机等不起更久（会卡住切歌），所以超时就当失败，跳过这首。
const providerTimeout = 8 * time.Second

// urlCacheTTL 是解析结果的进程内缓存时长，docs §5 要求 ≤5 分钟。
// 网易云播放地址本身带过期时间，缓存久了会出现 403。
const urlCacheTTL = 4 * time.Minute

type urlEntry struct {
	url   string
	cover string
	exp   time.Time
}

// urlCache 是"source:id → 播放地址"的简单缓存。
//
// 只缓存 URL，不缓存 Track 其它字段；容量按房间规模估算很小，
// 不做 LRU——过期的条目在读取时顺带清掉即可。
type urlCache struct {
	mu sync.Mutex
	m  map[string]urlEntry
}

func newURLCache() *urlCache { return &urlCache{m: make(map[string]urlEntry)} }

func (c *urlCache) get(key string) (urlEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.exp) {
		delete(c.m, key)
		return urlEntry{}, false
	}
	return e, true
}

func (c *urlCache) put(key, url, cover string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = urlEntry{url: url, cover: cover, exp: time.Now().Add(urlCacheTTL)}
	// 防止极端情况下无限增长：超过 4096 条就整体清空（重新解析成本可接受）。
	if len(c.m) > 4096 {
		c.m = make(map[string]urlEntry)
	}
}

func trackKey(t model.Track) string {
	return t.Source + ":" + t.ID
}

// resolveTrack 填充 t.URL（必要时补 cover），带缓存。
//
// 会先复制一份再交给 provider，避免 provider 实现意外改动调用方的
// 队列元素；成功后才写回。
func (h *Hub) resolveTrack(ctx context.Context, t *model.Track) error {
	if t == nil || strings.TrimSpace(t.ID) == "" {
		return errors.New("曲目缺少 ID")
	}
	if t.Source == "" {
		t.Source = h.primary
	}
	if e, ok := h.urls.get(trackKey(*t)); ok {
		t.URL = e.url
		if t.Cover == "" {
			t.Cover = e.cover
		}
		return nil
	}
	p, ok := h.reg.ForTrack(*t)
	if !ok {
		return fmt.Errorf("未知音乐源 %q", t.Source)
	}
	cp := *t
	callCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	if err := p.Resolve(callCtx, &cp); err != nil {
		return err
	}
	if strings.TrimSpace(cp.URL) == "" {
		return fmt.Errorf("provider %s 没有返回播放地址", p.Name())
	}
	if cp.Source == "" {
		cp.Source = p.Name()
	}
	h.urls.put(trackKey(cp), cp.URL, cp.Cover)
	*t = cp
	return nil
}

// fillLyric 给「当前播放」的曲目补 LRC 歌词。
//
// 按契约（docs §2）只有当前曲目带 lyric，所以不能放进 resolveTrack：
// 队列里其它曲目在解析预取时不该拉歌词。歌词失败/为空都忽略，
// 绝不能因为歌词而让播放失败。
func (h *Hub) fillLyric(ctx context.Context, t *model.Track) {
	if t == nil || t.Lyric != "" {
		return
	}
	p, ok := h.reg.ForTrack(*t)
	if !ok {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	var lrc string
	var err error
	if lp, ok := p.(provider.LyricProvider); ok {
		if full, ferr := lp.LyricFull(callCtx, t.ID); ferr == nil {
			lrc, err = full.Lrc, nil
			if strings.TrimSpace(full.Trans) != "" {
				t.LyricTrans = full.Trans
			}
		} else {
			err = ferr
		}
	} else {
		lrc, err = p.Lyric(callCtx, t.ID)
	}
	if err != nil || strings.TrimSpace(lrc) == "" {
		return
	}
	t.Lyric = lrc
}

// playlistTracks 拉取一个歌单的曲目（电台补歌用）。
func (h *Hub) playlistTracks(ctx context.Context, source, playlistID string) (model.Playlist, []model.Track, error) {
	p, ok := h.reg.For(source)
	if !ok {
		return model.Playlist{}, nil, fmt.Errorf("未知音乐源 %q", source)
	}
	callCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	pl, tracks, err := p.Playlist(callCtx, playlistID)
	if err != nil {
		return model.Playlist{}, nil, err
	}
	// 搜索/歌单结果不带 URL（docs §5），顺便把空 URL 清掉防止误用。
	for i := range tracks {
		tracks[i].URL = ""
		if tracks[i].Source == "" {
			tracks[i].Source = p.Name()
		}
	}
	return pl, tracks, nil
}

// searchTracks 搜歌曲。
func (h *Hub) searchTracks(ctx context.Context, source, q string, page, size int) ([]model.Track, int, error) {
	p, ok := h.reg.For(source)
	if !ok {
		return nil, 0, fmt.Errorf("未知音乐源 %q", source)
	}
	callCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	return p.Search(callCtx, q, page, size)
}

// playlistSearch 是 playlist.search 的兜底实现。
//
// provider.Provider 接口（server/internal/provider/provider.go）没有歌单搜索方法，
// 所以这里用可选接口：实现了 SearchPlaylists 的 provider（demo，或将来补充的
// netease）走真实搜索，没实现的返回空列表而不是报错——前端拿到空列表可以
// 正常显示"没有结果"，不会打断"按歌单 ID 导入"主流程。
func (h *Hub) playlistSearch(ctx context.Context, source, q string, page, size int) ([]model.Playlist, int, error) {
	p, ok := h.reg.For(source)
	if !ok {
		return nil, 0, fmt.Errorf("未知音乐源 %q", source)
	}
	type playlistSearcher interface {
		SearchPlaylists(ctx context.Context, q string, page, size int) ([]model.Playlist, int, error)
	}
	if ps, ok := p.(playlistSearcher); ok {
		callCtx, cancel := context.WithTimeout(ctx, providerTimeout)
		defer cancel()
		return ps.SearchPlaylists(callCtx, q, page, size)
	}
	return []model.Playlist{}, 0, nil
}

// playlistGet 取歌单详情（playlist.get）。
func (h *Hub) playlistGet(ctx context.Context, source, id string) (model.Playlist, []model.Track, error) {
	p, ok := h.reg.For(source)
	if !ok {
		return model.Playlist{}, nil, fmt.Errorf("未知音乐源 %q", source)
	}
	callCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	pl, tracks, err := p.Playlist(callCtx, id)
	if err != nil {
		return model.Playlist{}, nil, err
	}
	for i := range tracks {
		tracks[i].URL = ""
		if tracks[i].Source == "" {
			tracks[i].Source = p.Name()
		}
	}
	return pl, tracks, nil
}

// userPlaylists 取用户公开歌单（user.playlists）。
func (h *Hub) userPlaylists(ctx context.Context, source, uid string) ([]model.Playlist, error) {
	p, ok := h.reg.For(source)
	if !ok {
		return nil, fmt.Errorf("未知音乐源 %q", source)
	}
	callCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	return p.UserPlaylists(callCtx, uid)
}
