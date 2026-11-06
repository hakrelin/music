// Package netease 是网易云音乐 provider（source = "wy"）。
//
// 纯标准库实现 weapi 协议（AES-128-CBC 两次 + 原始 RSA 一次），支持：
//   - 搜索歌曲
//   - 歌单详情（tracks 不全时按 trackIds 批量补第一批 200 首）
//   - 用户创建/收藏的公开歌单
//   - 播放地址解析（320k 优先，退回 128k，强制 https）
//   - LRC 歌词
//
// cookie 从配置注入：
//   - 有有效 cookie：cloudsearch、VIP 曲目的高码率地址都正常；
//   - 无 cookie（或 cookie 失效）：cloudsearch 会被网易云拒绝（code=50000005），
//     Search 会自动降级到老接口 /weapi/search/get，所以匿名也能搜索；
//     免费曲目仍能 Resolve，VIP 曲目则返回带上下文的错误，由调用方跳过。
package netease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider"
)

// sourceName 是注册表里的 source 前缀（wy = 网易云）。
const sourceName = "wy"

// 单次搜索 / 歌单最多补多少首曲目。
//
// 实测 /weapi/v3/playlist/detail 即使 n=1000 也只回 20 首 tracks，
// 必须按 trackIds 用 /weapi/v3/song/detail 批量补；补满 200 首足够电台播放，
// 再大会显著增加单次导入耗时。
const (
	maxPlaylistFillTracks = 200
	songDetailChunkSize   = 100
	maxSearchPageSize     = 100 // cloudsearch 的 limit 上限
)

// defaultSearchPageSize 是 size 不合法时的默认值。
const defaultSearchPageSize = 30

// Config 是 provider 配置。
type Config struct {
	// Cookie 是 music.163.com 的登录 Cookie 原文（MUSIC_U=...; __csrf=...）。
	Cookie string
	// TimeoutSeconds 是单次请求超时，默认 10。
	TimeoutSeconds int
}

// Provider 实现 provider.Provider。
type Provider struct {
	cfg     Config
	client  *http.Client
	csrf    string // 从 cookie 里提取的 __csrf，部分 weapi 接口要求与 cookie 一致
	baseURL string // weapi 入口，默认 weapiBaseURL；单测可替换成 httptest 地址
}

// New 构造 provider；没有 cookie 时仍可搜索，但播放地址可能受限。
func New(cfg Config) *Provider {
	cfg.Cookie = sanitizeCookie(cfg.Cookie)

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Provider{
		cfg:     cfg,
		client:  &http.Client{Timeout: timeout},
		csrf:    csrfFromCookie(cfg.Cookie),
		baseURL: weapiBaseURL,
	}
}

// Name 实现 provider.Provider。
func (p *Provider) Name() string { return sourceName }

// Search 按关键词搜歌曲，返回曲目与总数。
//
// 正常路径走 cloudsearch/get/web（字段最全：al/ar/dt/fee/privilege）。
// 云音乐对匿名请求的 cloudsearch 会直接回 code=50000005，所以：
//   - 无 cookie 时直接走老接口 search/get；
//   - 有 cookie 但 cloudsearch 明确业务拒绝（多为 cookie 过期）时，也降级到老接口；
//   - 网络故障不降级（降级也会失败，徒增延迟）。
func (p *Provider) Search(ctx context.Context, q string, page, size int) ([]model.Track, int, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, 0, errors.New("netease.Search: 搜索关键词为空")
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = defaultSearchPageSize
	}
	if size > maxSearchPageSize {
		size = maxSearchPageSize
	}

	if p.cfg.Cookie != "" {
		tracks, total, err := p.searchCloud(ctx, q, page, size)
		if err == nil {
			return tracks, total, nil
		}
		var apiErr *weapiError
		if !errors.As(err, &apiErr) {
			// 网络/协议层错误：重试已在 weapi 里做过，这里直接上报。
			return nil, 0, fmt.Errorf("netease.Search(%q, page=%d, size=%d): %w", q, page, size, err)
		}
		// 业务拒绝（cookie 失效最常见）：退回老接口，保证搜索不中断。
		tracks, total, legacyErr := p.searchLegacy(ctx, q, page, size)
		if legacyErr != nil {
			return nil, 0, fmt.Errorf("netease.Search(%q, page=%d, size=%d): cloudsearch 被拒（%v），老接口也失败: %w", q, page, size, err, legacyErr)
		}
		return tracks, total, nil
	}

	tracks, total, err := p.searchLegacy(ctx, q, page, size)
	if err != nil {
		return nil, 0, fmt.Errorf("netease.Search(%q, page=%d, size=%d): %w", q, page, size, err)
	}
	return tracks, total, nil
}

// searchCloud 是 cloudsearch/get/web（type=1）的新搜索接口。
func (p *Provider) searchCloud(ctx context.Context, q string, page, size int) ([]model.Track, int, error) {
	payload := map[string]any{
		"s":      q,
		"type":   1, // 1 = 单曲
		"limit":  size,
		"offset": (page - 1) * size,
		"total":  true,
	}
	var resp struct {
		Result struct {
			Songs     []wySong `json:"songs"`
			SongCount int      `json:"songCount"`
		} `json:"result"`
	}
	if err := p.weapi(ctx, "cloudsearch/get/web", payload, &resp); err != nil {
		return nil, 0, err
	}
	tracks := make([]model.Track, 0, len(resp.Result.Songs))
	for _, s := range resp.Result.Songs {
		tracks = append(tracks, s.toTrack(nil))
	}
	return tracks, resp.Result.SongCount, nil
}

// searchLegacy 是老搜索接口 /weapi/search/get（匿名可用）。
//
// 它的 song 对象只有 album/artists/duration/fee，没有封面；因此拿到 id 后
// 再用 /weapi/v3/song/detail 补一次 al/ar/dt（失败则退回老字段，封面为空但不影响点歌）。
func (p *Provider) searchLegacy(ctx context.Context, q string, page, size int) ([]model.Track, int, error) {
	payload := map[string]any{
		"s":      q,
		"type":   1,
		"limit":  size,
		"offset": (page - 1) * size,
		"total":  true,
	}
	var resp struct {
		Result struct {
			Songs     []wySong `json:"songs"`
			SongCount int      `json:"songCount"`
		} `json:"result"`
	}
	if err := p.weapi(ctx, "search/get", payload, &resp); err != nil {
		return nil, 0, err
	}

	songs := resp.Result.Songs
	if len(songs) == 0 {
		return []model.Track{}, resp.Result.SongCount, nil
	}

	// 用 song/detail 补封面等字段；补失败不致命，老字段照样能出 Track。
	ids := make([]int64, 0, len(songs))
	for _, s := range songs {
		ids = append(ids, s.ID)
	}
	enriched, fees, err := p.songsByIDs(ctx, ids)
	if err != nil {
		enriched = nil
		fees = nil
	}
	return mergeSongsByID(songs, enriched, fees), resp.Result.SongCount, nil
}

// SearchPlaylists 按关键词搜歌单（cloudsearch type=1000）。
//
// hub 通过「可选接口」探测它：实现了该方法，wy 模式前端就能搜歌单。
// 降级策略与 Search 相同：无 cookie 或 cloudsearch 被拒（cookie 失效返回 50000005）时，
// 退回老接口 /weapi/search/get（type=1000）；两者都把歌单放在 result.playlists。
func (p *Provider) SearchPlaylists(ctx context.Context, q string, page, size int) ([]model.Playlist, int, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, 0, errors.New("netease.SearchPlaylists: 搜索关键词为空")
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = defaultSearchPageSize
	}
	if size > maxSearchPageSize {
		size = maxSearchPageSize
	}

	if p.cfg.Cookie != "" {
		playlists, total, err := p.searchPlaylistsCloud(ctx, q, page, size)
		if err == nil {
			return playlists, total, nil
		}
		var apiErr *weapiError
		if !errors.As(err, &apiErr) {
			return nil, 0, fmt.Errorf("netease.SearchPlaylists(%q, page=%d, size=%d): %w", q, page, size, err)
		}
		playlists, total, legacyErr := p.searchPlaylistsLegacy(ctx, q, page, size)
		if legacyErr != nil {
			return nil, 0, fmt.Errorf("netease.SearchPlaylists(%q, page=%d, size=%d): cloudsearch 被拒（%v），老接口也失败: %w", q, page, size, err, legacyErr)
		}
		return playlists, total, nil
	}

	playlists, total, err := p.searchPlaylistsLegacy(ctx, q, page, size)
	if err != nil {
		return nil, 0, fmt.Errorf("netease.SearchPlaylists(%q, page=%d, size=%d): %w", q, page, size, err)
	}
	return playlists, total, nil
}

// searchPlaylistsCloud 是 cloudsearch/get/web type=1000 的歌单搜索。
func (p *Provider) searchPlaylistsCloud(ctx context.Context, q string, page, size int) ([]model.Playlist, int, error) {
	payload := map[string]any{
		"s":      q,
		"type":   1000, // 1000 = 歌单
		"limit":  size,
		"offset": (page - 1) * size,
		"total":  true,
	}
	var resp struct {
		Result struct {
			Playlists     []wyPlaylist `json:"playlists"`
			PlaylistCount int          `json:"playlistCount"`
		} `json:"result"`
	}
	if err := p.weapi(ctx, "cloudsearch/get/web", payload, &resp); err != nil {
		return nil, 0, err
	}
	return playlistsToModels(resp.Result.Playlists), resp.Result.PlaylistCount, nil
}

// searchPlaylistsLegacy 是老搜索接口 /weapi/search/get 的歌单搜索（匿名可用）。
func (p *Provider) searchPlaylistsLegacy(ctx context.Context, q string, page, size int) ([]model.Playlist, int, error) {
	payload := map[string]any{
		"s":      q,
		"type":   1000,
		"limit":  size,
		"offset": (page - 1) * size,
		"total":  true,
	}
	var resp struct {
		Result struct {
			Playlists     []wyPlaylist `json:"playlists"`
			PlaylistCount int          `json:"playlistCount"`
		} `json:"result"`
	}
	if err := p.weapi(ctx, "search/get", payload, &resp); err != nil {
		return nil, 0, err
	}
	return playlistsToModels(resp.Result.Playlists), resp.Result.PlaylistCount, nil
}

// Playlist 取歌单详情与前 200 首曲目。
//
// 实测 v3/playlist/detail 的 playlist.tracks 只有 20 首（即使 n=1000），
// 所以以 trackIds 为准：直接批量取 song/detail 补第一批，再按 trackIds 顺序重排。
func (p *Provider) Playlist(ctx context.Context, id string) (model.Playlist, []model.Track, error) {
	playlistID, err := parseNumericID(id)
	if err != nil {
		return model.Playlist{}, nil, fmt.Errorf("netease.Playlist: %w", err)
	}
	var resp struct {
		Playlist   *wyPlaylist   `json:"playlist"`
		Privileges []wyPrivilege `json:"privileges"`
	}
	// n=1000 让接口尽量多回曲目；s=8 是网页端沿用的参数字节位，缺它返回更少。
	if err := p.weapi(ctx, "v3/playlist/detail", map[string]any{"id": playlistID, "n": 1000, "s": 8}, &resp); err != nil {
		return model.Playlist{}, nil, fmt.Errorf("netease.Playlist(%s): %w", id, err)
	}
	if resp.Playlist == nil {
		return model.Playlist{}, nil, fmt.Errorf("netease.Playlist(%s): 歌单不存在或不可见", id)
	}

	pl := resp.Playlist.toModel()
	tracks, fillErr := p.playlistTracks(ctx, resp.Playlist, resp.Privileges)
	if fillErr != nil {
		if len(tracks) == 0 {
			return pl, nil, fmt.Errorf("netease.Playlist(%s): %w", id, fillErr)
		}
		// 详情接口已经带回部分曲目时降级返回，别让整个歌单不可用。
	}
	return pl, tracks, nil
}

// playlistTracks 以 trackIds 顺序组织曲目；tracks 明显不全时用 song/detail 补第一批。
func (p *Provider) playlistTracks(ctx context.Context, wp *wyPlaylist, privileges []wyPrivilege) ([]model.Track, error) {
	feeByID := privilegeFees(privileges)
	ids := wp.trackIDList()

	// 详情自带的 tracks 是否够用：不足 min(trackIds, 200) 就补。
	needFill := len(ids) > 0 && len(wp.Tracks) < minInt(len(ids), maxPlaylistFillTracks)
	if !needFill {
		return songsToTracks(wp.Tracks, feeByID), nil
	}

	limit := minInt(len(ids), maxPlaylistFillTracks)
	songs, fees, err := p.songsByIDs(ctx, ids[:limit])
	if err != nil {
		if len(wp.Tracks) > 0 {
			return songsToTracks(wp.Tracks, feeByID), nil
		}
		return nil, fmt.Errorf("按 trackIds 补齐曲目失败: %w", err)
	}
	for songID, fee := range fees {
		if _, ok := feeByID[songID]; !ok {
			feeByID[songID] = fee
		}
	}

	// 以 trackIds 顺序为准（详情里的 tracks 顺序未必与歌单一致），
	// 再把详情中多出来的曲目按原顺序追加。
	byID := make(map[int64]wySong, len(songs))
	for _, s := range songs {
		byID[s.ID] = s
	}
	out := make([]model.Track, 0, len(songs)+len(wp.Tracks))
	seen := make(map[int64]struct{}, len(songs)+len(wp.Tracks))
	for _, songID := range ids[:limit] {
		if s, ok := byID[songID]; ok {
			out = append(out, s.toTrack(feeByID))
			seen[songID] = struct{}{}
		}
	}
	for _, s := range wp.Tracks {
		if _, ok := seen[s.ID]; ok {
			continue
		}
		out = append(out, s.toTrack(feeByID))
		seen[s.ID] = struct{}{}
	}
	return out, nil
}

// UserPlaylists 取某用户创建/收藏的公开歌单。
func (p *Provider) UserPlaylists(ctx context.Context, uid string) ([]model.Playlist, error) {
	userID, err := parseNumericID(uid)
	if err != nil {
		return nil, fmt.Errorf("netease.UserPlaylists: %w", err)
	}
	var resp struct {
		Playlist []wyPlaylist `json:"playlist"`
		More     bool         `json:"more"`
	}
	if err := p.weapi(ctx, "user/playlist", map[string]any{"uid": userID, "offset": 0, "limit": 1000}, &resp); err != nil {
		return nil, fmt.Errorf("netease.UserPlaylists(%s): %w", uid, err)
	}
	out := make([]model.Playlist, 0, len(resp.Playlist))
	for _, wp := range resp.Playlist {
		out = append(out, wp.toModel())
	}
	return out, nil
}

// Resolve 填 Track.URL：先要 320k，失败再要 128k；URL 统一强制 https。
// 均失败时返回带上下文（曲名/fee）的错误，调用方据此跳过该曲。
func (p *Provider) Resolve(ctx context.Context, t *model.Track) error {
	if t == nil {
		return errors.New("netease.Resolve: track 为空")
	}
	if t.Source == "" {
		t.Source = sourceName
	}
	songID, err := parseNumericID(t.ID)
	if err != nil {
		return fmt.Errorf("netease.Resolve: %w", err)
	}
	url, _, err := p.resolveURL(ctx, songID)
	if err != nil {
		return fmt.Errorf("netease.Resolve: 曲目 %s《%s》无法解析播放地址（fee=%d）: %w", t.ID, t.Name, t.Fee, err)
	}
	t.URL = url
	return nil
}

// resolveURL 依次尝试 320k、128k，返回第一个非空地址。
func (p *Provider) resolveURL(ctx context.Context, songID int64) (string, int, error) {
	var lastErr error
	for _, br := range []int{320000, 128000} {
		url, gotBr, err := p.songURL(ctx, songID, br)
		if err != nil {
			lastErr = err
			continue
		}
		if url != "" {
			return url, gotBr, nil
		}
		lastErr = fmt.Errorf("br=%d 返回空地址（可能无版权或需要 VIP）", br)
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的播放地址")
	}
	return "", 0, lastErr
}

// songURL 请求单曲的播放地址；返回的 URL 已强制成 https（网易云有时给 http）。
func (p *Provider) songURL(ctx context.Context, songID int64, br int) (string, int, error) {
	// ids 字段是「JSON 数组字符串」而不是数组本身，网页端就是这么传的。
	payload := map[string]any{
		"ids": fmt.Sprintf("[%d]", songID),
		"br":  br,
	}
	var resp struct {
		Data []struct {
			URL  string `json:"url"`
			Br   int    `json:"br"`
			Code int    `json:"code"`
		} `json:"data"`
	}
	if err := p.weapi(ctx, "song/enhance/player/url", payload, &resp); err != nil {
		return "", 0, fmt.Errorf("请求播放地址失败: %w", err)
	}
	if len(resp.Data) == 0 {
		return "", 0, nil
	}
	return forceHTTPS(resp.Data[0].URL), resp.Data[0].Br, nil
}

// Lyric 取 LRC 歌词原文；没有歌词时返回空串而不是错误。
func (p *Provider) Lyric(ctx context.Context, id string) (string, error) {
	songID, err := parseNumericID(id)
	if err != nil {
		return "", fmt.Errorf("netease.Lyric: %w", err)
	}
	var resp struct {
		Lrc struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
	}
	// lv/kv/tv 都传 -1 表示要原版 + 翻译 + 罗马音（接口会按可用性返回）。
	if err := p.weapi(ctx, "song/lyric", map[string]any{"id": songID, "lv": -1, "kv": -1, "tv": -1}, &resp); err != nil {
		return "", fmt.Errorf("netease.Lyric(%s): %w", id, err)
	}
	return resp.Lrc.Lyric, nil
}

// ---- 网易云响应结构 ----

type wyArtist struct {
	Name string `json:"name"`
}

type wyAlbum struct {
	Name   string `json:"name"`
	PicURL string `json:"picUrl"`
}

type wyPrivilege struct {
	ID  int64 `json:"id"`
	Fee int   `json:"fee"`
}

// wySong 同时兼容新接口字段（ar/al/dt）与老接口字段（artists/album/duration）：
// cloudsearch、song/detail 用新字段，search/get 用老字段，三个入口共用一种结构体。
type wySong struct {
	ID        int64        `json:"id"`
	Name      string       `json:"name"`
	Ar        []wyArtist   `json:"ar"`
	Artists   []wyArtist   `json:"artists"`
	Al        *wyAlbum     `json:"al"`
	Album     *wyAlbum     `json:"album"`
	Dt        int64        `json:"dt"`       // 毫秒
	Duration  int64        `json:"duration"` // 毫秒（老接口）
	Fee       int          `json:"fee"`
	Privilege *wyPrivilege `json:"privilege"`
}

type wyTrackID struct {
	ID int64 `json:"id"`
}

type wyCreator struct {
	Nickname string `json:"nickname"`
}

type wyPlaylist struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	CoverImgURL string      `json:"coverImgUrl"`
	Creator     wyCreator   `json:"creator"`
	TrackCount  int         `json:"trackCount"`
	TrackIDs    []wyTrackID `json:"trackIds"`
	Tracks      []wySong    `json:"tracks"`
}

// toTrack 把网易云 song 映射成共享 Track。
// fee 的取值优先级：privileges 数组（按 id 匹配）> 内嵌 privilege > song.fee。
func (s wySong) toTrack(feeByID map[int64]int) model.Track {
	artists := s.Ar
	if len(artists) == 0 {
		artists = s.Artists
	}
	names := make([]string, 0, len(artists))
	for _, a := range artists {
		if name := strings.TrimSpace(a.Name); name != "" {
			names = append(names, name)
		}
	}

	album := s.Al
	if album == nil {
		album = s.Album
	}
	var albumName, cover string
	if album != nil {
		albumName = album.Name
		// 老接口的封面有时是 http，过一遍 forceHTTPS 免得 https 页面被混合内容拦截。
		cover = forceHTTPS(album.PicURL)
	}

	millis := s.Dt
	if millis <= 0 {
		millis = s.Duration
	}

	fee, known := 0, false
	if feeByID != nil {
		fee, known = feeByID[s.ID]
	}
	if !known && s.Privilege != nil {
		fee, known = s.Privilege.Fee, true
	}
	if !known {
		fee = s.Fee
	}

	return model.Track{
		ID:       strconv.FormatInt(s.ID, 10),
		Source:   sourceName,
		Name:     s.Name,
		Artist:   strings.Join(names, "/"),
		Album:    albumName,
		Cover:    cover,
		Duration: int(millis / 1000),
		Fee:      fee,
	}
}

// trackIDList 提取 trackIds 里的 id（过滤掉异常 0）。
func (wp *wyPlaylist) trackIDList() []int64 {
	out := make([]int64, 0, len(wp.TrackIDs))
	for _, t := range wp.TrackIDs {
		if t.ID > 0 {
			out = append(out, t.ID)
		}
	}
	return out
}

func (wp *wyPlaylist) toModel() model.Playlist {
	count := wp.TrackCount
	if count <= 0 {
		count = len(wp.TrackIDs)
	}
	return model.Playlist{
		ID:   strconv.FormatInt(wp.ID, 10),
		Name: wp.Name,
		// 老接口返回的封面可能是 http，统一转 https。
		Cover:   forceHTTPS(wp.CoverImgURL),
		Creator: wp.Creator.Nickname,
		Count:   count,
	}
}

// privilegeFees 把 privileges 数组转成 id→fee 映射。
func privilegeFees(privileges []wyPrivilege) map[int64]int {
	fees := make(map[int64]int, len(privileges))
	for _, p := range privileges {
		fees[p.ID] = p.Fee
	}
	return fees
}

// songsToTracks 批量转换（保持输入顺序）。
func songsToTracks(songs []wySong, feeByID map[int64]int) []model.Track {
	out := make([]model.Track, 0, len(songs))
	for _, s := range songs {
		out = append(out, s.toTrack(feeByID))
	}
	return out
}

// playlistsToModels 批量转换歌单（保持输入顺序）。
func playlistsToModels(playlists []wyPlaylist) []model.Playlist {
	out := make([]model.Playlist, 0, len(playlists))
	for _, wp := range playlists {
		out = append(out, wp.toModel())
	}
	return out
}

// mergeSongsByID 用 enriched 覆盖 base 中的同 id 曲目（补封面等字段），保持 base 顺序。
func mergeSongsByID(base, enriched []wySong, feeByID map[int64]int) []model.Track {
	byID := make(map[int64]wySong, len(enriched))
	for _, s := range enriched {
		byID[s.ID] = s
	}
	out := make([]model.Track, 0, len(base))
	for _, s := range base {
		if e, ok := byID[s.ID]; ok {
			out = append(out, e.toTrack(feeByID))
			continue
		}
		out = append(out, s.toTrack(nil))
	}
	return out
}

type songIDParam struct {
	ID int64 `json:"id"`
}

// songsByIDs 用 /weapi/v3/song/detail 批量取歌曲详情（每批 100 首），
// 返回歌曲与 id→fee 映射。这是补封面/补歌单曲目的统一入口。
func (p *Provider) songsByIDs(ctx context.Context, ids []int64) ([]wySong, map[int64]int, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	songs := make([]wySong, 0, len(ids))
	fees := make(map[int64]int, len(ids))
	for start := 0; start < len(ids); start += songDetailChunkSize {
		end := minInt(start+songDetailChunkSize, len(ids))
		params := make([]songIDParam, 0, end-start)
		for _, id := range ids[start:end] {
			params = append(params, songIDParam{ID: id})
		}
		// c 字段是「JSON 字符串」：形如 `[{"id":123},{"id":456}]`。
		cParam, err := json.Marshal(params)
		if err != nil {
			return nil, nil, fmt.Errorf("编码 song/detail 参数失败: %w", err)
		}
		var resp struct {
			Songs      []wySong      `json:"songs"`
			Privileges []wyPrivilege `json:"privileges"`
		}
		if err := p.weapi(ctx, "v3/song/detail", map[string]any{"c": string(cParam)}, &resp); err != nil {
			return nil, nil, fmt.Errorf("song/detail 批次 %d-%d 失败: %w", start, end, err)
		}
		songs = append(songs, resp.Songs...)
		for _, pr := range resp.Privileges {
			fees[pr.ID] = pr.Fee
		}
	}
	return songs, fees, nil
}

// ---- 小工具 ----

// sanitizeCookie 去掉 cookie 里的换行并裁剪空白：http.Header 不接受包含换行的值。
func sanitizeCookie(cookie string) string {
	cookie = strings.ReplaceAll(cookie, "\r", "")
	cookie = strings.ReplaceAll(cookie, "\n", "")
	return strings.TrimSpace(cookie)
}

// csrfFromCookie 从 cookie 中提取 __csrf（weapi 的 csrf_token 参数要用）。
func csrfFromCookie(cookie string) string {
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if value, ok := strings.CutPrefix(part, "__csrf="); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// parseNumericID 解析网易云的数字 id；兼容用户直接粘贴的歌单/用户主页链接。
// 只接受正整数，避免把 `?id=-1` 之类非法输入传给接口。
func parseNumericID(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, errors.New("ID 为空")
	}
	if id, err := strconv.ParseInt(s, 10, 64); err == nil && id > 0 {
		return id, nil
	}

	// 链接形态（https://music.163.com/playlist?id=2416955681 或 ...&id=...）：
	// 优先取最后一个 "id=" 后面的连续数字，别把域名里的 163 当成 id。
	if id, ok := digitsAfterQueryID(s); ok {
		return id, nil
	}

	// 路径形态（.../playlist/2416955681）：取最后一段连续数字。
	// 跳过负数（"-5" 这种输入不应被当成 id=5）。
	lastStart, lastEnd := -1, -1
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			continue
		}
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if start > 0 && s[start-1] == '-' {
			continue
		}
		lastStart, lastEnd = start, i
	}
	if lastStart >= 0 {
		if id, err := strconv.ParseInt(s[lastStart:lastEnd], 10, 64); err == nil && id > 0 {
			return id, nil
		}
	}
	return 0, fmt.Errorf("非法 ID %q（应为数字，或包含数字的歌单/用户链接）", raw)
}

// digitsAfterQueryID 从 "...id=123..." 里提取 123。
func digitsAfterQueryID(s string) (int64, bool) {
	idx := strings.LastIndex(strings.ToLower(s), "id=")
	if idx < 0 {
		return 0, false
	}
	rest := s[idx+len("id="):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	id, err := strconv.ParseInt(rest[:end], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// forceHTTPS 把网易云返回的 http 地址强制成 https：前端页面本身是 https 时
// 混用 http 音频会被浏览器按混合内容拦截。无协议的 //host/path 也补成 https。
func forceHTTPS(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	switch {
	case u == "":
		return ""
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "HTTPS://"):
		return u
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "HTTP://"):
		return "https://" + u[len("http://"):]
	case strings.HasPrefix(u, "//"):
		return "https:" + u
	default:
		return u
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 编译期断言：netease.Provider 必须满足 provider.Provider 接口（契约见 docs/architecture.md §5）。
var _ provider.Provider = (*Provider)(nil)
