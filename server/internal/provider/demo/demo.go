// Package demo 是一个完全离线的音乐源实现（source = "demo"）。
//
// 它存在的意义：任何人 clone 下来 `go run` 就能看到完整 UI、播放、切歌、
// 同步和电台补歌，不需要网易云 cookie，也不会因为外部接口抖动而无法演示。
//
// 曲目、歌单、封面、音频全部由本包生成，其中音频是程序合成的柔和和弦垫
// （44.1kHz / 16bit / 单声道 WAV），见 audio.go。
package demo

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hakrelin/music/server/internal/model"
)

// PlaylistID 是内置演示歌单的 ID。radio.load / playlist.get 都用它。
const PlaylistID = "demo-radio"

// playlist 是内置演示歌单的元信息。
var playlist = model.Playlist{
	ID:      PlaylistID,
	Name:    "东方电台（演示）",
	Cover:   "/api/demo/cover/playlist.svg",
	Creator: "茶与月色",
	Count:   24,
}

// song 是曲目的静态声明（不含 URL / 点赞等运行时字段）。
type song struct {
	id       string
	name     string
	artist   string
	album    string
	duration int // 秒，同时是合成音频的长度
}

// songs 是 24 首虚构曲目。名字都带东方 Project 的意象，但旋律是程序合成的，
// 所以它们不是任何真实作品——演示数据不该被误当成版权内容。
var songs = []song{
	{"d01", "月下竹林", "上海アリス幻樂団（演示）", "幻想乡组曲・壹", 32},
	{"d02", "红魔馆的午后", "上海アリス幻樂団（演示）", "幻想乡组曲・壹", 38},
	{"d03", "永远亭的雨", "黄昏フロンティア（演示）", "幻想乡组曲・壹", 41},
	{"d04", "博丽神社的黄昏", "上海アリス幻樂団（演示）", "幻想乡组曲・壹", 35},
	{"d05", "魔法森林的萤火", "茶与月色（演示）", "幻想乡组曲・壹", 44},
	{"d06", "守矢的风", "黄昏フロンティア（演示）", "幻想乡组曲・贰", 30},
	{"d07", "白玉楼的雪", "上海アリス幻樂団（演示）", "幻想乡组曲・贰", 42},
	{"d08", "旧地狱的灯火", "茶与月色（演示）", "幻想乡组曲・贰", 37},
	{"d09", "地灵殿的低语", "黄昏フロンティア（演示）", "幻想乡组曲・贰", 39},
	{"d10", "妖怪之山的晨雾", "上海アリス幻樂団（演示）", "幻想乡组曲・贰", 33},
	{"d11", "星莲船的夜航", "茶与月色（演示）", "幻想乡组曲・贰", 45},
	{"d12", "命莲寺的钟声", "上海アリス幻樂団（演示）", "幻想乡组曲・贰", 36},
	{"d13", "太阳花田的午后", "黄昏フロンティア（演示）", "幻想乡组曲・叁", 34},
	{"d14", "三途川的渡船", "茶与月色（演示）", "幻想乡组曲・叁", 40},
	{"d15", "冥界的樱花", "上海アリス幻樂団（演示）", "幻想乡组曲・叁", 43},
	{"d16", "天狗的羽翼", "黄昏フロンティア（演示）", "幻想乡组曲・叁", 31},
	{"d17", "河童的机关匣", "茶与月色（演示）", "幻想乡组曲・叁", 38},
	{"d18", "觉之瞳", "上海アリス幻樂団（演示）", "幻想乡组曲・叁", 44},
	{"d19", "恋之瞳", "黄昏フロンティア（演示）", "幻想乡组曲・肆", 32},
	{"d20", "四季的庭院", "茶与月色（演示）", "幻想乡组曲・肆", 41},
	{"d21", "八云家的茶会", "上海アリス幻樂団（演示）", "幻想乡组曲・肆", 36},
	{"d22", "月都的银色回廊", "黄昏フロンティア（演示）", "幻想乡组曲・肆", 45},
	{"d23", "幻想乡的黎明", "茶与月色（演示）", "幻想乡组曲・肆", 33},
	{"d24", "茶与月色的终章", "上海アリス幻樂団（演示）", "幻想乡组曲・肆", 39},
}

// Provider 实现 provider.Provider。
type Provider struct {
	tracks []model.Track
	byID   map[string]model.Track
}

// New 构造 demo provider；数据全部在内存里，不会失败。
func New() *Provider {
	p := &Provider{
		tracks: make([]model.Track, 0, len(songs)),
		byID:   make(map[string]model.Track, len(songs)),
	}
	for _, s := range songs {
		t := model.Track{
			ID:       s.id,
			Source:   "demo",
			Name:     s.name,
			Artist:   s.artist,
			Album:    s.album,
			Cover:    "/api/demo/cover/" + s.id + ".svg",
			Duration: s.duration,
			Fee:      0,
		}
		p.tracks = append(p.tracks, t)
		p.byID[s.id] = t
	}
	return p
}

// Name 实现 provider.Provider。
func (p *Provider) Name() string { return "demo" }

// Search 按子串匹配曲名/歌手/专辑。q 为空时返回全部（分页）。
func (p *Provider) Search(_ context.Context, q string, page, size int) ([]model.Track, int, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 50 {
		size = 50
	}
	q = strings.ToLower(strings.TrimSpace(q))

	matched := make([]model.Track, 0, len(p.tracks))
	for _, t := range p.tracks {
		if q == "" ||
			strings.Contains(strings.ToLower(t.Name), q) ||
			strings.Contains(strings.ToLower(t.Artist), q) ||
			strings.Contains(strings.ToLower(t.Album), q) {
			matched = append(matched, t)
		}
	}

	total := len(matched)
	start := (page - 1) * size
	if start >= total {
		return nil, total, nil
	}
	end := start + size
	if end > total {
		end = total
	}
	return matched[start:end], total, nil
}

// Playlist 返回内置演示歌单。
//
// 注意：demo 模式刻意"宽容"——任何非空 id 都返回内置歌单。这样当配置里
// 写的是网易云歌单 ID（默认 config.example.json 就是），demo 模式下
// radio.load / 补歌依然能跑通，不会因为 provider 不匹配而空队列。
func (p *Provider) Playlist(_ context.Context, id string) (model.Playlist, []model.Track, error) {
	if strings.TrimSpace(id) == "" {
		return model.Playlist{}, nil, fmt.Errorf("demo: 歌单 ID 为空")
	}
	pl := playlist
	pl.Count = len(p.tracks)
	return pl, append([]model.Track(nil), p.tracks...), nil
}

// UserPlaylists 返回两个演示歌单，供前端"按用户 ID 导入"联调。
func (p *Provider) UserPlaylists(_ context.Context, uid string) ([]model.Playlist, error) {
	// uid 只用于展示，demo 不校验；空 uid 也返回演示数据。
	_ = uid
	return p.userPlaylists(), nil
}

func (p *Provider) userPlaylists() []model.Playlist {
	return []model.Playlist{
		{ID: PlaylistID, Name: playlist.Name, Cover: playlist.Cover, Creator: "茶与月色", Count: len(p.tracks)},
		{ID: "demo-night", Name: "夜晚的幻想乡（演示）", Cover: "/api/demo/cover/playlist-night.svg", Creator: "茶与月色", Count: len(p.tracks)},
	}
}

// SearchPlaylists 实现 hub 侧的可选歌单搜索接口。
//
// provider.Provider 接口没有歌单搜索方法（见 provider.go），所以
// playlist.search 用可选接口探测；demo 提供实现让前端链路完整。
func (p *Provider) SearchPlaylists(_ context.Context, q string, page, size int) ([]model.Playlist, int, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	if size > 50 {
		size = 50
	}
	q = strings.ToLower(strings.TrimSpace(q))
	all := p.userPlaylists()
	matched := make([]model.Playlist, 0, len(all))
	for _, pl := range all {
		if q == "" || strings.Contains(strings.ToLower(pl.Name), q) || strings.Contains(strings.ToLower(pl.Creator), q) {
			matched = append(matched, pl)
		}
	}
	total := len(matched)
	start := (page - 1) * size
	if start >= total {
		return []model.Playlist{}, total, nil
	}
	end := start + size
	if end > total {
		end = total
	}
	return matched[start:end], total, nil
}

// Resolve 填播放地址与封面。demo 的地址是本站的合成音频路由。
func (p *Provider) Resolve(_ context.Context, t *model.Track) error {
	if t == nil {
		return fmt.Errorf("demo: 空曲目")
	}
	id := strings.TrimSpace(t.ID)
	if id == "" {
		return fmt.Errorf("demo: 曲目 ID 为空")
	}
	s, ok := p.byID[id]
	if !ok {
		return fmt.Errorf("demo: 未知曲目 %q", id)
	}
	if t.Source == "" {
		t.Source = "demo"
	}
	// 补全服务端才知道的静态字段，避免前端拿到的 Track 缺信息。
	if t.Name == "" {
		t.Name = s.Name
	}
	if t.Artist == "" {
		t.Artist = s.Artist
	}
	if t.Album == "" {
		t.Album = s.Album
	}
	if t.Cover == "" {
		t.Cover = s.Cover
	}
	if t.Duration <= 0 {
		t.Duration = s.Duration
	}
	t.URL = "/api/demo/audio/" + id + ".wav"
	return nil
}

// Lyric 返回内置 LRC 歌词（演示用，内容是氛围文案而不是真实歌词）。
func (p *Provider) Lyric(_ context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	s, ok := p.byID[id]
	if !ok {
		return "", fmt.Errorf("demo: 未知曲目 %q", id)
	}
	lines := []string{
		"[00:00.00]「" + s.Name + "」",
		"[00:02.00]" + s.Artist,
		"[00:06.00]月光落在虚拟的竹林上",
		"[00:12.00]风把旋律吹得很慢",
		"[00:18.00]— 这是程序合成的演示音频 —",
		"[00:24.00]茶与月色，一起听一首歌",
	}
	return strings.Join(lines, "\n"), nil
}

// HasTrack 判断 id 是否是 demo 曲目（音频/封面路由用它做 404 判定）。
func (p *Provider) HasTrack(id string) bool {
	_, ok := p.byID[id]
	return ok
}

// TrackIDs 返回全部曲目 ID（升序，测试与调试用）。
func (p *Provider) TrackIDs() []string {
	ids := make([]string, 0, len(p.byID))
	for id := range p.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
