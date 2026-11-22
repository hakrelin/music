// Package bilibili 是哔哩哔哩（B站）音乐源实现（source = "bili"）。
//
// 核心约束：
//   - 搜索视频（WBI 签名 /x/web-interface/wbi/search/type），同时支持直接输入 BV/AV 号或视频链接；
//   - 只抽取纯音频流（DASH dash.audio AAC 轨道：30280 192k / 30232 132k / 30216 64k），绝不拉取视频流；
//   - 默认使用登录态（UseLogin=true），Cookie 由配置文件或管理后台显式配置；
//   - 上游 CDN 校验 Referer，经 /api/stream/{id}?source=bili 透传 Range + Referer（命中对象存储时 302 直连 COS）。
package bilibili

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider"
)

const (
	sourceName      = "bili"
	defaultAPIBase  = "https://api.bilibili.com"
	defaultPassBase = "https://passport.bilibili.com"
	biliUA          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	streamCacheTTL  = 15 * time.Minute
)

// Config 是 B 站 provider 配置。
type Config struct {
	Cookie         string // 显式配置的 Cookie（SESSDATA=...; bili_jct=...; DedeUserID=...）
	UseLogin       bool   // 默认 true：强制使用登录态，避免匿名风控与低音质
	TimeoutSeconds int
}

type cachedStream struct {
	urls []string
	exp  time.Time
}

// Provider 实现 provider.Provider、DetailProvider、RefreshProvider、
// StreamProxyProvider、UpstreamAudioResolver。
type Provider struct {
	mu           sync.RWMutex
	cookie       string
	useLogin     bool
	client       *http.Client
	streamClient *http.Client
	apiBase      string
	passportBase string

	wbiMixin string
	wbiExp   time.Time
	buvid3   string
	buvid4   string
	buvidExp time.Time

	streams    map[string]cachedStream
	refreshSeq atomic.Uint64
}

// New 构造 B 站 provider。
func New(cfg Config) *Provider {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	return &Provider{
		cookie:       sanitizeCookie(cfg.Cookie),
		useLogin:     cfg.UseLogin,
		client:       &http.Client{Timeout: timeout},
		streamClient: &http.Client{Timeout: 0}, // 流式代理不设总超时，由请求上下文控制
		apiBase:      defaultAPIBase,
		passportBase: defaultPassBase,
		streams:      make(map[string]cachedStream),
	}
}

// Name 实现 provider.Provider。
func (p *Provider) Name() string { return sourceName }

// UpdateConfig 热更新 B 站配置（Cookie / 登录态开关），同时清理缓存的 WBI 密钥和音频流地址。
func (p *Provider) UpdateConfig(cfg Config) {
	p.mu.Lock()
	p.cookie = sanitizeCookie(cfg.Cookie)
	p.useLogin = cfg.UseLogin
	p.wbiMixin = ""
	p.wbiExp = time.Time{}
	p.streams = make(map[string]cachedStream)
	p.mu.Unlock()
}

// Cookie 返回当前显式配置的 B 站 Cookie。
func (p *Provider) Cookie() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cookie
}

// UseLogin 返回当前是否默认要求登录态。
func (p *Provider) UseLogin() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.useLogin
}

func (p *Provider) httpClient() *http.Client {
	if p.client != nil {
		return p.client
	}
	return http.DefaultClient
}

func (p *Provider) apiEndpoint(path string) string {
	base := p.apiBase
	if base == "" {
		base = defaultAPIBase
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func (p *Provider) passportEndpoint(path string) string {
	base := p.passportBase
	if base == "" {
		base = defaultPassBase
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

// requireLoginIfNeeded 在默认登录态开启时校验是否已显式配置含 SESSDATA 的 Cookie。
func (p *Provider) requireLoginIfNeeded() error {
	p.mu.RLock()
	useLogin := p.useLogin
	cookie := p.cookie
	p.mu.RUnlock()
	if !useLogin {
		return nil
	}
	if cookie == "" {
		return errors.New("B站默认使用登录态，请先在管理员页面登录B站或显式配置 Cookie")
	}
	if !hasCookieKey(cookie, "SESSDATA") {
		return errors.New("B站 Cookie 缺少 SESSDATA 登录凭证，请重新登录或检查 Cookie 配置")
	}
	return nil
}

// outgoingCookie 返回带 buvid3/buvid4 补齐的请求 Cookie 头。
func (p *Provider) outgoingCookie(ctx context.Context, includeBuvid bool) string {
	p.mu.RLock()
	cookie := p.cookie
	p.mu.RUnlock()
	if !includeBuvid {
		return cookie
	}
	if hasCookieKey(cookie, "buvid3") {
		return cookie
	}
	b3, b4 := p.ensureBuvid(ctx)
	if b3 == "" {
		return cookie
	}
	parts := []string{}
	if cookie != "" {
		parts = append(parts, cookie)
	}
	parts = append(parts, "buvid3="+b3)
	if b4 != "" && !hasCookieKey(cookie, "buvid4") {
		parts = append(parts, "buvid4="+b4)
	}
	return strings.Join(parts, "; ")
}

// Search 按关键词在 B 站搜索视频曲目；输入 BV/AV 号或视频链接时直接查询视频及分 P。
func (p *Provider) Search(ctx context.Context, q string, page, size int) ([]model.Track, int, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, 0, errors.New("搜索关键词不能为空")
	}
	if err := p.requireLoginIfNeeded(); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	if size > 30 {
		size = 30
	}

	// 直接输入 BV 号 / av 号 / 视频链接：直接拉取该视频（含多 P 分集）
	if bvid, aid, wantPage, ok := extractVideoRef(q); ok {
		tracks, err := p.lookupVideoTracks(ctx, bvid, aid, wantPage)
		if err != nil {
			return nil, 0, err
		}
		total := len(tracks)
		start := (page - 1) * size
		if start >= total {
			return []model.Track{}, total, nil
		}
		end := start + size
		if end > total {
			end = total
		}
		return tracks[start:end], total, nil
	}

	return p.searchByKeyword(ctx, q, page, size)
}

func (p *Provider) searchByKeyword(ctx context.Context, q string, page, size int) ([]model.Track, int, error) {
	cookie := p.outgoingCookie(ctx, true)
	params := url.Values{}
	params.Set("search_type", "video")
	params.Set("keyword", q)
	params.Set("page", strconv.Itoa(page))
	params.Set("page_size", strconv.Itoa(size))

	type searchResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			NumResults int `json:"numResults"`
			Result     []struct {
				BVid     string `json:"bvid"`
				Aid      int64  `json:"aid"`
				Title    string `json:"title"`
				Author   string `json:"author"`
				Pic      string `json:"pic"`
				Duration string `json:"duration"`
				TypeName string `json:"typename"`
			} `json:"result"`
		} `json:"data"`
	}

	var resp searchResp
	for attempt := 0; attempt < 2; attempt++ {
		mixin, err := p.ensureWBIKey(ctx)
		if err != nil {
			return nil, 0, err
		}
		qs := signWBIQuery(params, mixin, time.Now())
		endpoint := p.apiEndpoint("/x/web-interface/wbi/search/type") + "?" + qs
		resp = searchResp{}
		if err := p.getJSON(ctx, endpoint, cookie, &resp); err != nil {
			return nil, 0, fmt.Errorf("B站搜索请求失败: %w", err)
		}
		if resp.Code == -352 && attempt == 0 {
			p.invalidateWBI()
			continue
		}
		break
	}
	if resp.Code != 0 {
		if resp.Code == -412 {
			return nil, 0, fmt.Errorf("B站搜索触发风控（-412），请确认已登录有效 Cookie")
		}
		return nil, 0, fmt.Errorf("B站搜索返回错误（code=%d %s）", resp.Code, strings.TrimSpace(resp.Message))
	}

	tracks := make([]model.Track, 0, len(resp.Data.Result))
	for _, item := range resp.Data.Result {
		bvid := strings.TrimSpace(item.BVid)
		if bvid == "" {
			continue
		}
		album := "哔哩哔哩"
		if tn := strings.TrimSpace(item.TypeName); tn != "" {
			album = "B站 · " + tn
		}
		tracks = append(tracks, model.Track{
			ID:       bvid,
			Source:   sourceName,
			Name:     cleanBiliText(item.Title),
			Artist:   cleanBiliText(item.Author),
			Album:    album,
			Cover:    forceHTTPS(item.Pic),
			Duration: parseDuration(item.Duration),
			Fee:      0,
		})
	}
	total := resp.Data.NumResults
	if total < len(tracks) {
		total = len(tracks)
	}
	return tracks, total, nil
}

type viewData struct {
	BVid     string `json:"bvid"`
	Aid      int64  `json:"aid"`
	Title    string `json:"title"`
	Pic      string `json:"pic"`
	Duration int    `json:"duration"`
	Cid      int64  `json:"cid"`
	Owner    struct {
		Name string `json:"name"`
	} `json:"owner"`
	Pages []struct {
		Cid      int64  `json:"cid"`
		Page     int    `json:"page"`
		Part     string `json:"part"`
		Duration int    `json:"duration"`
	} `json:"pages"`
}

func (p *Provider) fetchView(ctx context.Context, bvid string, aid int64) (viewData, error) {
	q := url.Values{}
	if bvid != "" {
		q.Set("bvid", bvid)
	} else if aid > 0 {
		q.Set("aid", strconv.FormatInt(aid, 10))
	} else {
		return viewData{}, errors.New("缺少 BV 或 AV 号")
	}
	var resp struct {
		Code    int      `json:"code"`
		Message string   `json:"message"`
		Data    viewData `json:"data"`
	}
	if err := p.getJSON(ctx, p.apiEndpoint("/x/web-interface/view")+"?"+q.Encode(), p.outgoingCookie(ctx, true), &resp); err != nil {
		return viewData{}, fmt.Errorf("获取 B 站视频详情失败: %w", err)
	}
	if resp.Code != 0 {
		return viewData{}, fmt.Errorf("B站视频不可用（code=%d %s）", resp.Code, strings.TrimSpace(resp.Message))
	}
	return resp.Data, nil
}

func (p *Provider) lookupVideoTracks(ctx context.Context, bvid string, aid int64, wantPage int) ([]model.Track, error) {
	v, err := p.fetchView(ctx, bvid, aid)
	if err != nil {
		return nil, err
	}
	title := cleanBiliText(v.Title)
	artist := cleanBiliText(v.Owner.Name)
	cover := forceHTTPS(v.Pic)
	if len(v.Pages) <= 1 {
		return []model.Track{{
			ID:       v.BVid,
			Source:   sourceName,
			Name:     title,
			Artist:   artist,
			Album:    "哔哩哔哩",
			Cover:    cover,
			Duration: v.Duration,
		}}, nil
	}
	tracks := make([]model.Track, 0, len(v.Pages))
	for _, pg := range v.Pages {
		id := v.BVid
		if pg.Page > 1 {
			id = fmt.Sprintf("%s_p%d", v.BVid, pg.Page)
		}
		name := title
		if part := cleanBiliText(pg.Part); part != "" && part != title {
			name = fmt.Sprintf("%s (P%d %s)", title, pg.Page, part)
		}
		dur := pg.Duration
		if dur <= 0 {
			dur = v.Duration
		}
		tracks = append(tracks, model.Track{
			ID:       id,
			Source:   sourceName,
			Name:     name,
			Artist:   artist,
			Album:    "哔哩哔哩",
			Cover:    cover,
			Duration: dur,
		})
	}
	if wantPage > 1 && wantPage <= len(tracks) {
		chosen := tracks[wantPage-1]
		rest := append([]model.Track{chosen}, tracks[:wantPage-1]...)
		tracks = append(rest, tracks[wantPage:]...)
	}
	return tracks, nil
}

// TrackDetail 补齐单曲元数据（时长/封面/标题/UP主）。
func (p *Provider) TrackDetail(ctx context.Context, id string) (model.Track, error) {
	if err := p.requireLoginIfNeeded(); err != nil {
		return model.Track{}, err
	}
	bvid, pageNum, err := parseTrackID(id)
	if err != nil {
		return model.Track{}, err
	}
	v, err := p.fetchView(ctx, bvid, 0)
	if err != nil {
		return model.Track{}, err
	}
	return trackFromView(v, pageNum), nil
}

func trackFromView(v viewData, pageNum int) model.Track {
	title := cleanBiliText(v.Title)
	dur := v.Duration
	id := v.BVid
	if len(v.Pages) > 1 {
		idx := pageNum - 1
		if idx < 0 || idx >= len(v.Pages) {
			idx = 0
		}
		pg := v.Pages[idx]
		if pg.Page > 1 {
			id = fmt.Sprintf("%s_p%d", v.BVid, pg.Page)
		}
		if part := cleanBiliText(pg.Part); part != "" && part != title {
			title = fmt.Sprintf("%s (P%d %s)", title, pg.Page, part)
		}
		if pg.Duration > 0 {
			dur = pg.Duration
		}
	}
	return model.Track{
		ID:       id,
		Source:   sourceName,
		Name:     title,
		Artist:   cleanBiliText(v.Owner.Name),
		Album:    "哔哩哔哩",
		Cover:    forceHTTPS(v.Pic),
		Duration: dur,
	}
}

// Playlist 支持将带分 P 的 B 站视频作为歌单导入；其他输入提示走搜索点歌。
func (p *Provider) Playlist(ctx context.Context, id string) (model.Playlist, []model.Track, error) {
	if err := p.requireLoginIfNeeded(); err != nil {
		return model.Playlist{}, nil, err
	}
	bvid, aid, wantPage, ok := extractVideoRef(id)
	if !ok {
		return model.Playlist{}, nil, errors.New("B站请粘贴视频 BV/AV 链接，或在「搜索点歌」里切换至哔哩哔哩搜索")
	}
	v, err := p.fetchView(ctx, bvid, aid)
	if err != nil {
		return model.Playlist{}, nil, err
	}
	tracks, err := p.lookupVideoTracks(ctx, v.BVid, 0, wantPage)
	if err != nil {
		return model.Playlist{}, nil, err
	}
	return model.Playlist{
		ID:      v.BVid,
		Name:    cleanBiliText(v.Title),
		Cover:   forceHTTPS(v.Pic),
		Creator: cleanBiliText(v.Owner.Name),
		Count:   len(tracks),
	}, tracks, nil
}

// UserPlaylists B 站暂不提供匿名昵称歌单导入。
func (p *Provider) UserPlaylists(_ context.Context, _ string) ([]model.Playlist, error) {
	return nil, errors.New("B站暂不支持按用户昵称导入歌单，请直接在「搜索点歌」切换至哔哩哔哩")
}

// Lyric B 站视频无标准 LRC 歌词。
func (p *Provider) Lyric(_ context.Context, _ string) (string, error) { return "", nil }

// Resolve 解析 B 站纯音频流，并把播放地址指向同源 /api/stream/{id}?source=bili。
func (p *Provider) Resolve(ctx context.Context, t *model.Track) error {
	return p.resolveTrack(ctx, t, false, false)
}

// ResolveFresh 强制刷新上游 DASH 音频地址（lower=true 时优先 132k/64k 省流轨道）。
func (p *Provider) ResolveFresh(ctx context.Context, t *model.Track, lower bool) error {
	return p.resolveTrack(ctx, t, lower, true)
}

func (p *Provider) resolveTrack(ctx context.Context, t *model.Track, lower, forceFresh bool) error {
	if t == nil || strings.TrimSpace(t.ID) == "" {
		return errors.New("bilibili.Resolve: 曲目 ID 为空")
	}
	if err := p.requireLoginIfNeeded(); err != nil {
		return err
	}
	if t.Source == "" {
		t.Source = sourceName
	}
	meta, urls, err := p.resolveUpstreamCandidates(ctx, t.ID, lower, forceFresh)
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return errors.New("bilibili.Resolve: 未找到可用音频流")
	}
	if t.Name == "" {
		t.Name = meta.Name
	}
	if t.Artist == "" {
		t.Artist = meta.Artist
	}
	if t.Album == "" {
		t.Album = meta.Album
	}
	if t.Cover == "" {
		t.Cover = meta.Cover
	}
	if t.Duration <= 0 {
		t.Duration = meta.Duration
	}

	u := "/api/stream/" + url.PathEscape(t.ID) + "?source=" + sourceName
	if lower {
		u += "&lower=1"
	}
	if forceFresh {
		u += "&v=" + strconv.FormatUint(p.refreshSeq.Add(1), 10)
	}
	t.URL = u
	return nil
}

// ResolveUpstreamAudio 返回真实的 B 站 DASH 纯音频上游地址（供对象存储缓存下载）。
func (p *Provider) ResolveUpstreamAudio(ctx context.Context, id string, lower bool) (string, error) {
	if err := p.requireLoginIfNeeded(); err != nil {
		return "", err
	}
	_, urls, err := p.resolveUpstreamCandidates(ctx, id, lower, true)
	if err != nil {
		return "", err
	}
	if len(urls) == 0 {
		return "", errors.New("未找到可用 B 站纯音频流")
	}
	return urls[0], nil
}

type dashAudioItem struct {
	ID        int      `json:"id"`
	BaseURL   string   `json:"baseUrl"`
	BaseURL2  string   `json:"base_url"`
	BackupURL []string `json:"backupUrl"`
	Backup2   []string `json:"backup_url"`
	Bandwidth int      `json:"bandwidth"`
	MimeType  string   `json:"mimeType"`
	Codecs    string   `json:"codecs"`
}

func (p *Provider) streamCacheKey(id string, lower bool) string {
	if lower {
		return id + ":lower"
	}
	return id
}

func (p *Provider) getCachedStream(id string, lower bool) ([]string, bool) {
	key := p.streamCacheKey(id, lower)
	p.mu.RLock()
	e, ok := p.streams[key]
	p.mu.RUnlock()
	if !ok || time.Now().After(e.exp) || len(e.urls) == 0 {
		return nil, false
	}
	return append([]string(nil), e.urls...), true
}

func (p *Provider) putCachedStream(id string, lower bool, urls []string) {
	key := p.streamCacheKey(id, lower)
	p.mu.Lock()
	if len(p.streams) > 512 {
		p.streams = make(map[string]cachedStream)
	}
	p.streams[key] = cachedStream{
		urls: append([]string(nil), urls...),
		exp:  time.Now().Add(streamCacheTTL),
	}
	p.mu.Unlock()
}

func (p *Provider) resolveUpstreamCandidates(ctx context.Context, id string, lower, forceFresh bool) (model.Track, []string, error) {
	if !forceFresh {
		if urls, ok := p.getCachedStream(id, lower); ok {
			return model.Track{ID: id, Source: sourceName}, urls, nil
		}
	}
	bvid, pageNum, err := parseTrackID(id)
	if err != nil {
		return model.Track{}, nil, err
	}
	v, err := p.fetchView(ctx, bvid, 0)
	if err != nil {
		return model.Track{}, nil, err
	}
	cid := v.Cid
	if len(v.Pages) > 0 {
		idx := pageNum - 1
		if idx < 0 || idx >= len(v.Pages) {
			idx = 0
		}
		if v.Pages[idx].Cid > 0 {
			cid = v.Pages[idx].Cid
		}
	}
	if cid <= 0 {
		return model.Track{}, nil, fmt.Errorf("B站视频 %s 缺少 cid", bvid)
	}
	meta := trackFromView(v, pageNum)

	cookie := p.outgoingCookie(ctx, true)
	params := url.Values{}
	params.Set("bvid", v.BVid)
	params.Set("cid", strconv.FormatInt(cid, 10))
	// fnval=16 请求标准 DASH，仅取 dash.audio 的浏览器原生兼容 AAC 音轨，不下载视频流。
	params.Set("fnval", "16")
	params.Set("fnver", "0")
	params.Set("fourk", "1")

	type playResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Dash *struct {
				Audio []dashAudioItem `json:"audio"`
			} `json:"dash"`
		} `json:"data"`
	}

	var resp playResp
	for attempt := 0; attempt < 2; attempt++ {
		mixin, err := p.ensureWBIKey(ctx)
		if err != nil {
			return meta, nil, err
		}
		qs := signWBIQuery(params, mixin, time.Now())
		endpoint := p.apiEndpoint("/x/player/wbi/playurl") + "?" + qs
		resp = playResp{}
		if err := p.getJSON(ctx, endpoint, cookie, &resp); err != nil {
			return meta, nil, fmt.Errorf("获取 B 站音频流失败: %w", err)
		}
		if resp.Code == -352 && attempt == 0 {
			p.invalidateWBI()
			continue
		}
		break
	}
	if resp.Code != 0 {
		return meta, nil, fmt.Errorf("B站音频解析失败（code=%d %s）", resp.Code, strings.TrimSpace(resp.Message))
	}
	if resp.Data.Dash == nil || len(resp.Data.Dash.Audio) == 0 {
		return meta, nil, errors.New("该 B 站视频未提供纯音频 DASH 流")
	}

	urls := selectAudioURLs(resp.Data.Dash.Audio, lower)
	if len(urls) == 0 {
		return meta, nil, errors.New("B站音频流地址为空")
	}
	p.putCachedStream(id, lower, urls)
	return meta, urls, nil
}

// selectAudioURLs 仅从 dash.audio 中挑选浏览器原生兼容的 AAC 纯音频流（绝不选视频轨）。
//   - 标准播放（lower=false）：30280 (192k) > 30232 (132k) > 30216 (64k) > 其他按码率降序；
//   - 省流/缓存预热（lower=true）：30232 (132k) > 30216 (64k) > 30280 (192k) > 其他按码率升序。
func selectAudioURLs(items []dashAudioItem, lower bool) []string {
	cands := append([]dashAudioItem(nil), items...)
	rank := func(id int) int {
		if lower {
			switch id {
			case 30232:
				return 0
			case 30216:
				return 1
			case 30280:
				return 2
			default:
				return 3
			}
		}
		switch id {
		case 30280:
			return 0
		case 30232:
			return 1
		case 30216:
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		ri, rj := rank(cands[i].ID), rank(cands[j].ID)
		if ri != rj {
			return ri < rj
		}
		if lower {
			return cands[i].Bandwidth < cands[j].Bandwidth
		}
		return cands[i].Bandwidth > cands[j].Bandwidth
	})

	seen := make(map[string]struct{})
	out := make([]string, 0, len(cands)*2)
	add := func(raw string) {
		u := forceHTTPS(raw)
		if u == "" {
			return
		}
		if _, dup := seen[u]; dup {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	for _, it := range cands {
		if it.BaseURL != "" {
			add(it.BaseURL)
		} else {
			add(it.BaseURL2)
		}
		for _, bu := range it.BackupURL {
			add(bu)
		}
		for _, bu := range it.Backup2 {
			add(bu)
		}
	}
	return out
}

// ServeAudio 带 Referer 与 Range 透传 B 站纯音频流，实现 provider.StreamProxyProvider。
func (p *Provider) ServeAudio(w http.ResponseWriter, r *http.Request, id string, lower bool) bool {
	if err := p.requireLoginIfNeeded(); err != nil {
		return false
	}
	ctx := r.Context()
	urls, cached := p.getCachedStream(id, lower)
	if !cached {
		rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		var err error
		_, urls, err = p.resolveUpstreamCandidates(rctx, id, lower, true)
		cancel()
		if err != nil || len(urls) == 0 {
			return false
		}
	}

	if p.proxyFromCandidates(w, r, urls) {
		return true
	}
	// 如果刚才用的是缓存签名且全部 403/过期，现场刷新一次再试。
	if cached {
		rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		_, freshURLs, err := p.resolveUpstreamCandidates(rctx, id, lower, true)
		cancel()
		if err == nil && len(freshURLs) > 0 {
			return p.proxyFromCandidates(w, r, freshURLs)
		}
	}
	return false
}

func (p *Provider) proxyFromCandidates(w http.ResponseWriter, r *http.Request, urls []string) bool {
	method := r.Method
	if method != http.MethodHead {
		method = http.MethodGet
	}
	client := p.streamClient
	if client == nil {
		client = http.DefaultClient
	}
	for _, target := range urls {
		req, err := http.NewRequestWithContext(r.Context(), method, target, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Referer", "https://www.bilibili.com/")
		req.Header.Set("Origin", "https://www.bilibili.com")
		req.Header.Set("User-Agent", biliUA)
		if rng := r.Header.Get("Range"); rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := client.Do(req)
		if err != nil {
			if r.Context().Err() != nil {
				return false
			}
			continue
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			_ = resp.Body.Close()
			continue
		}
		ctype := resp.Header.Get("Content-Type")
		if ctype == "" || strings.HasPrefix(ctype, "application/octet-stream") {
			ctype = "audio/mp4"
		}
		w.Header().Set("Content-Type", ctype)
		if ar := resp.Header.Get("Accept-Ranges"); ar != "" {
			w.Header().Set("Accept-Ranges", ar)
		} else {
			w.Header().Set("Accept-Ranges", "bytes")
		}
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			w.Header().Set("Content-Range", cr)
		}
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			w.Header().Set("Content-Length", cl)
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(resp.StatusCode)
		if method != http.MethodHead {
			_, _ = io.Copy(w, resp.Body)
		}
		_ = resp.Body.Close()
		return true
	}
	return false
}

// DownloadAudio 拉取整段 B 站音频（供对象存储缓存预热用），带 Referer 且限制最大字节数。
func DownloadAudio(ctx context.Context, rawURL string, limit int64) (data []byte, truncated bool, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("Origin", "https://www.bilibili.com")
	req.Header.Set("User-Agent", biliUA)
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, false, fmt.Errorf("B站音频上游返回 HTTP %d", resp.StatusCode)
	}
	buf := make([]byte, 0, 1<<20)
	chunk := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			if int64(len(buf)) > limit {
				return nil, true, nil
			}
		}
		if rerr == io.EOF {
			return buf, false, nil
		}
		if rerr != nil {
			return nil, false, rerr
		}
	}
}

// ---- 解析与清洗工具 ----

var (
	reBV      = regexp.MustCompile(`(?i)\b(BV1[0-9A-Za-z]{9})\b`)
	reAV      = regexp.MustCompile(`(?i)\bav(\d{1,12})\b`)
	rePageQ   = regexp.MustCompile(`(?i)[?&]p=(\d{1,4})`)
	reHTMLTag = regexp.MustCompile(`<[^>]+>`)
)

func extractVideoRef(raw string) (bvid string, aid int64, page int, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", 0, 0, false
	}
	page = 1
	if pm := rePageQ.FindStringSubmatch(s); len(pm) == 2 {
		if n, err := strconv.Atoi(pm[1]); err == nil && n > 0 {
			page = n
		}
	}
	if m := reBV.FindStringSubmatch(s); len(m) == 2 {
		// 仅当输入本身是 BV 号或 B 站链接时直接按视频解析，避免误拦截普通标题搜索
		if strings.EqualFold(s, m[1]) || strings.Contains(strings.ToLower(s), "bilibili.com") || strings.Contains(s, "_p") {
			if idx := strings.LastIndex(s, "_p"); idx > 0 {
				if n, err := strconv.Atoi(s[idx+2:]); err == nil && n > 0 {
					page = n
				}
			}
			return "BV" + m[1][2:], 0, page, true
		}
	}
	if m := reAV.FindStringSubmatch(s); len(m) == 2 {
		if strings.EqualFold(s, "av"+m[1]) || strings.Contains(strings.ToLower(s), "bilibili.com") {
			if id, err := strconv.ParseInt(m[1], 10, 64); err == nil && id > 0 {
				return "", id, page, true
			}
		}
	}
	return "", 0, 0, false
}

func parseTrackID(id string) (bvid string, page int, err error) {
	s := strings.TrimSpace(id)
	page = 1
	if base, suffix, ok := strings.Cut(s, "_p"); ok {
		s = base
		if n, perr := strconv.Atoi(suffix); perr == nil && n > 0 {
			page = n
		}
	}
	if !reBV.MatchString(s) || len(s) != 12 {
		return "", 0, fmt.Errorf("非法的 B 站曲目 ID %q", id)
	}
	return "BV" + s[2:], page, nil
}

func cleanBiliText(raw string) string {
	noTag := reHTMLTag.ReplaceAllString(raw, "")
	return strings.TrimSpace(html.UnescapeString(noTag))
}

func parseDuration(raw string) int {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n
	}
	parts := strings.Split(s, ":")
	total := 0
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return total
}

func forceHTTPS(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	switch {
	case u == "":
		return ""
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "HTTPS://"):
		return u
	case strings.HasPrefix(u, "http://127.0.0.1"), strings.HasPrefix(u, "http://localhost"), strings.HasPrefix(u, "http://[::1]"):
		return u
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "HTTP://"):
		return "https://" + u[len("http://"):]
	case strings.HasPrefix(u, "//"):
		return "https:" + u
	default:
		return u
	}
}

func sanitizeCookie(cookie string) string {
	cookie = strings.ReplaceAll(cookie, "\r", "")
	cookie = strings.ReplaceAll(cookie, "\n", "")
	return strings.TrimSpace(cookie)
}

func hasCookieKey(cookie, key string) bool {
	for _, part := range strings.Split(cookie, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), key) && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

var (
	_ provider.Provider              = (*Provider)(nil)
	_ provider.DetailProvider        = (*Provider)(nil)
	_ provider.RefreshProvider       = (*Provider)(nil)
	_ provider.StreamProxyProvider   = (*Provider)(nil)
	_ provider.UpstreamAudioResolver = (*Provider)(nil)
)
