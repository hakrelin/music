package netease

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hakrelin/music/server/internal/model"
)

// TestWeapiEncryptRoundTrip 验证加密自洽：params 能用 secret + 固定 key 解回原始 JSON。
// 这是 weapi 最容易写错的地方（两次 AES 的输入、固定 IV、RSA 无填充）。
func TestWeapiEncryptRoundTrip(t *testing.T) {
	payload := map[string]any{"s": "东方", "type": 1, "limit": 3, "offset": 0, "total": true}

	params, encSecKey, secret, err := weapiEncrypt(payload)
	if err != nil {
		t.Fatalf("weapiEncrypt 失败: %v", err)
	}
	if len(secret) != secretLen {
		t.Fatalf("secret 长度应为 %d，实际 %d", secretLen, len(secret))
	}
	if len(encSecKey) != rsaBlockBytes*2 {
		t.Fatalf("encSecKey 应为 %d 个十六进制字符，实际 %d", rsaBlockBytes*2, len(encSecKey))
	}
	if _, err := hex.DecodeString(encSecKey); err != nil {
		t.Fatalf("encSecKey 不是合法十六进制: %v", err)
	}

	// 第二次 AES 用 secret 解，第一次 AES 用固定 key 解。
	once, err := aesDecryptBase64([]byte(params), secret)
	if err != nil {
		t.Fatalf("用 secret 解密 params 失败: %v", err)
	}
	plain, err := aesDecryptBase64(once, []byte(weapiPresetKey))
	if err != nil {
		t.Fatalf("用固定 key 解密失败: %v", err)
	}

	wantJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化原始参数失败: %v", err)
	}
	var want, got any
	if err := json.Unmarshal(wantJSON, &want); err != nil {
		t.Fatalf("解析原始 JSON 失败: %v", err)
	}
	if err := json.Unmarshal(plain, &got); err != nil {
		t.Fatalf("解密结果不是合法 JSON: %v（原文 %q）", err, string(plain))
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("解密结果与原文不一致:\n want=%v\n got=%v", want, got)
	}

	// 同样的 payload + secret 必须得到完全相同的 params/encSecKey（协议是确定性的）。
	params2, key2, err := encryptParams(payload, secret)
	if err != nil {
		t.Fatalf("encryptParams 第二次调用失败: %v", err)
	}
	if params2 != params || key2 != encSecKey {
		t.Fatalf("固定 secret 下加密结果应可复现")
	}

	// secret 不同则结果必须不同（防止误用固定 secret）。
	params3, _, err := encryptParams(payload, []byte("0123456789abcdef"))
	if err != nil {
		t.Fatalf("换 secret 加密失败: %v", err)
	}
	if params3 == params {
		t.Fatalf("不同 secret 得到相同 params，加密实现可疑")
	}
}

// TestWeapiRetryThenSuccess 验证：空响应/5xx 会重试（共 3 次请求），成功后正常解析。
func TestWeapiRetryThenSuccess(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("解析表单失败: %v", err)
		}
		if r.Form.Get("params") == "" {
			t.Errorf("请求缺少 params")
		}
		if got := len(r.Form.Get("encSecKey")); got != rsaBlockBytes*2 {
			t.Errorf("encSecKey 长度应为 %d，实际 %d", rsaBlockBytes*2, got)
		}
		if r.Header.Get("Referer") != "https://music.163.com/" {
			t.Errorf("Referer 应为 https://music.163.com/，实际 %q", r.Header.Get("Referer"))
		}
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("缺少 User-Agent")
		}
		if r.Header.Get("Cookie") != "MUSIC_U=test; __csrf=csrftest" {
			t.Errorf("Cookie 未透传: %q", r.Header.Get("Cookie"))
		}
		switch n {
		case 1:
			// 模拟网易云对坏参数的响应：HTTP 200 + 空 body。
			w.WriteHeader(http.StatusOK)
		case 2:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":500}`))
		default:
			_, _ = w.Write([]byte(`{"code":200,"lrc":{"lyric":"[00:00.00]测试歌词"}}`))
		}
	}))
	defer server.Close()

	p := New(Config{Cookie: "MUSIC_U=test; __csrf=csrftest", TimeoutSeconds: 5})
	p.baseURL = server.URL + "/weapi/"

	lyric, err := p.Lyric(context.Background(), "123")
	if err != nil {
		t.Fatalf("重试后仍失败: %v", err)
	}
	if lyric != "[00:00.00]测试歌词" {
		t.Fatalf("歌词解析错误: %q", lyric)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("应重试 2 次（共 3 次请求），实际 %d 次", got)
	}
}

// TestWeapiBusinessErrorNoRetry 验证业务错误（code != 200）不重试，并保留错误码。
func TestWeapiBusinessErrorNoRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"code":400,"msg":"参数错误"}`))
	}))
	defer server.Close()

	p := New(Config{TimeoutSeconds: 5})
	p.baseURL = server.URL + "/weapi/"

	_, err := p.Lyric(context.Background(), "123")
	if err == nil {
		t.Fatal("业务错误应返回 error")
	}
	var apiErr *weapiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("应保留 *weapiError，实际 %T: %v", err, err)
	}
	if apiErr.Code != 400 {
		t.Fatalf("错误码应为 400，实际 %d", apiErr.Code)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("业务错误不应重试，实际请求 %d 次", got)
	}
}

// TestNoCookieDoesNotPanic 验证无 cookie 时的降级行为：构造、非法参数都只返回错误。
func TestNoCookieDoesNotPanic(t *testing.T) {
	p := New(Config{})
	if p.Name() != "wy" {
		t.Fatalf("Name 应为 wy，实际 %q", p.Name())
	}
	if p.client == nil || p.client.Timeout != 10*time.Second {
		t.Fatalf("默认超时应为 10s，实际 %v", p.client.Timeout)
	}
	if p.csrf != "" {
		t.Fatalf("无 cookie 时 csrf 应为空，实际 %q", p.csrf)
	}

	ctx := context.Background()
	if err := p.Resolve(ctx, nil); err == nil {
		t.Fatal("Resolve(nil) 应返回 error")
	}
	if err := p.Resolve(ctx, &model.Track{}); err == nil {
		t.Fatal("空 ID 的 Resolve 应返回 error")
	}
	if _, err := p.Lyric(ctx, ""); err == nil {
		t.Fatal("空 ID 的 Lyric 应返回 error")
	}
	if _, _, err := p.Playlist(ctx, "abc"); err == nil {
		t.Fatal("非法歌单 ID 应返回 error")
	}
	if _, err := p.UserPlaylists(ctx, "  "); err == nil {
		t.Fatal("空 uid 应返回 error")
	}
}

func TestForceHTTPS(t *testing.T) {
	cases := map[string]string{
		"http://m10.music.126.net/a.mp3":   "https://m10.music.126.net/a.mp3",
		"https://m701.music.126.net/a.mp3": "https://m701.music.126.net/a.mp3",
		"//m801.music.126.net/a.mp3":       "https://m801.music.126.net/a.mp3",
		"":                                 "",
		"not-a-url":                        "not-a-url",
	}
	for in, want := range cases {
		if got := forceHTTPS(in); got != want {
			t.Errorf("forceHTTPS(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestParseNumericID(t *testing.T) {
	ok := map[string]int64{
		"2416955681": 2416955681,
		"  737955  ": 737955,
		"https://music.163.com/playlist?id=2416955681":  2416955681,
		"https://music.163.com/user/home?id=4873352070": 4873352070,
	}
	for in, want := range ok {
		got, err := parseNumericID(in)
		if err != nil {
			t.Errorf("parseNumericID(%q) 返回错误: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseNumericID(%q) = %d，期望 %d", in, got, want)
		}
	}
	for _, in := range []string{"", "  ", "abc", "0", "-5"} {
		if got, err := parseNumericID(in); err == nil {
			t.Errorf("parseNumericID(%q) 应返回错误，实际得到 %d", in, got)
		}
	}
}

// ---- 用 httptest 的离线集成测试（不依赖公网，验证映射与降级逻辑） ----

// newMockProvider 起一个 httptest 服务，并让 Provider 的 weapi 入口指向它。
func newMockProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	p := New(Config{TimeoutSeconds: 5})
	p.baseURL = server.URL + "/weapi/"
	return p
}

// TestSearchMapsCloudSearch 验证 cloudsearch 成功路径的字段映射（含 privilege.fee）。
func TestSearchMapsCloudSearch(t *testing.T) {
	var cloudCalls, legacyCalls atomic.Int32
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cloudsearch/get/web"):
			cloudCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"result":{"songCount":42,"songs":[
				{"id":737955,"name":"曲一","ar":[{"name":"歌手A"},{"name":"歌手B"}],
				 "al":{"name":"专辑","picUrl":"https://p1.music.126.net/c.jpg"},"dt":326293,"fee":8,
				 "privilege":{"id":737955,"fee":8}}
			]}}`))
		case strings.HasSuffix(r.URL.Path, "/search/get"):
			legacyCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"result":{"songCount":0,"songs":[]}}`))
		default:
			http.NotFound(w, r)
		}
	})
	p.cfg.Cookie = "MUSIC_U=ok; __csrf=x" // 有 cookie 时优先 cloudsearch

	tracks, total, err := p.Search(context.Background(), "东方", 1, 5)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if total != 42 || len(tracks) != 1 {
		t.Fatalf("total/len 不符：total=%d len=%d", total, len(tracks))
	}
	tr := tracks[0]
	if tr.ID != "737955" || tr.Name != "曲一" || tr.Source != "wy" {
		t.Fatalf("基础字段映射错误: %+v", tr)
	}
	if tr.Artist != "歌手A/歌手B" {
		t.Fatalf("多歌手应使用 / 连接，实际 %q", tr.Artist)
	}
	if tr.Album != "专辑" || tr.Cover == "" {
		t.Fatalf("专辑/封面映射错误: album=%q cover=%q", tr.Album, tr.Cover)
	}
	if tr.Duration != 326 {
		t.Fatalf("duration 应毫秒转秒 326，实际 %d", tr.Duration)
	}
	if tr.Fee != 8 {
		t.Fatalf("fee 应取 privilege.fee=8，实际 %d", tr.Fee)
	}
	if cloudCalls.Load() != 1 || legacyCalls.Load() != 0 {
		t.Fatalf("有 cookie 时应只走 cloudsearch：cloud=%d legacy=%d", cloudCalls.Load(), legacyCalls.Load())
	}
}

// TestSearchCloudRejectedFallsBackToLegacy 验证 cookie 失效时 Search 自动降级到老接口。
func TestSearchCloudRejectedFallsBackToLegacy(t *testing.T) {
	var cloudCalls, legacyCalls, detailCalls atomic.Int32
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cloudsearch/get/web"):
			cloudCalls.Add(1)
			// 实测 cookie 失效 / 匿名访问 cloudsearch 时网易云返回这个码。
			_, _ = w.Write([]byte(`{"code":50000005}`))
		case strings.HasSuffix(r.URL.Path, "/search/get"):
			legacyCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"result":{"songCount":1,"songs":[
				{"id":7,"name":"老接口曲目","artists":[{"name":"歌手"}],
				 "album":{"name":"老专辑"},"duration":61000,"fee":4}
			]}}`))
		case strings.HasSuffix(r.URL.Path, "/v3/song/detail"):
			detailCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"songs":[
				{"id":7,"name":"老接口曲目","ar":[{"name":"歌手"}],
				 "al":{"name":"老专辑","picUrl":"https://p1.music.126.net/x.jpg"},"dt":61000,"fee":4}
			],"privileges":[{"id":7,"fee":4}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	p.cfg.Cookie = "MUSIC_U=expired; __csrf=x"

	tracks, total, err := p.Search(context.Background(), "东方", 1, 5)
	if err != nil {
		t.Fatalf("降级后 Search 仍失败: %v", err)
	}
	if total != 1 || len(tracks) != 1 {
		t.Fatalf("降级结果不符：total=%d len=%d", total, len(tracks))
	}
	if tracks[0].Name != "老接口曲目" || tracks[0].Cover == "" {
		t.Fatalf("降级映射错误（封面应由 song/detail 补齐）: %+v", tracks[0])
	}
	if tracks[0].Duration != 61 || tracks[0].Fee != 4 {
		t.Fatalf("降级字段映射错误: %+v", tracks[0])
	}
	if cloudCalls.Load() != 1 || legacyCalls.Load() != 1 || detailCalls.Load() != 1 {
		t.Fatalf("请求次数不符：cloud=%d legacy=%d detail=%d",
			cloudCalls.Load(), legacyCalls.Load(), detailCalls.Load())
	}
}

// TestSearchAnonymousSkipsCloudSearch 验证无 cookie 时直接走老接口（不去撞必败的 cloudsearch）。
func TestSearchAnonymousSkipsCloudSearch(t *testing.T) {
	var cloudCalls, legacyCalls atomic.Int32
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cloudsearch/get/web"):
			cloudCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":50000005}`))
		case strings.HasSuffix(r.URL.Path, "/search/get"):
			legacyCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"result":{"songCount":1,"songs":[
				{"id":7,"name":"匿名曲目","artists":[{"name":"歌手"}],"album":{"name":"专辑"},"duration":61000,"fee":0}
			]}}`))
		case strings.HasSuffix(r.URL.Path, "/v3/song/detail"):
			_, _ = w.Write([]byte(`{"code":200,"songs":[],"privileges":[]}`))
		default:
			http.NotFound(w, r)
		}
	})

	tracks, total, err := p.Search(context.Background(), "东方", 1, 5)
	if err != nil || total != 1 || len(tracks) != 1 {
		t.Fatalf("匿名搜索失败: err=%v total=%d len=%d", err, total, len(tracks))
	}
	if cloudCalls.Load() != 0 || legacyCalls.Load() != 1 {
		t.Fatalf("无 cookie 时不应请求 cloudsearch：cloud=%d legacy=%d", cloudCalls.Load(), legacyCalls.Load())
	}
}

// TestPlaylistFillsTracksFromTrackIDs 验证 tracks 为空时按 trackIds 批量补齐并保持顺序。
func TestPlaylistFillsTracksFromTrackIDs(t *testing.T) {
	var detailCalls, songCalls atomic.Int32
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/v3/playlist/detail"):
			detailCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,
				"playlist":{"id":2416955681,"name":"东方Project-原曲","coverImgUrl":"https://p1.music.126.net/cover.jpg",
					"creator":{"nickname":"鱼"},"trackCount":3,
					"trackIds":[{"id":11},{"id":22},{"id":33}],"tracks":[]},
				"privileges":[{"id":11,"fee":0},{"id":22,"fee":8}]}`))
		case strings.HasSuffix(r.URL.Path, "/v3/song/detail"):
			songCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"songs":[
				{"id":11,"name":"A","ar":[{"name":"X"},{"name":"Y"}],"al":{"name":"AL","picUrl":"https://p1.music.126.net/a.jpg"},"dt":245000,"fee":0},
				{"id":22,"name":"B","ar":[{"name":"Z"}],"al":{"name":"AL","picUrl":"https://p1.music.126.net/a.jpg"},"dt":120000,"fee":8},
				{"id":33,"name":"C","ar":[{"name":"W"}],"al":{"name":"AL","picUrl":"https://p1.music.126.net/a.jpg"},"dt":60000,"fee":0}
			],"privileges":[{"id":11,"fee":0},{"id":22,"fee":8},{"id":33,"fee":0}]}`))
		default:
			http.NotFound(w, r)
		}
	})

	pl, tracks, err := p.Playlist(context.Background(), "2416955681")
	if err != nil {
		t.Fatalf("Playlist 失败: %v", err)
	}
	if pl.ID != "2416955681" || pl.Name != "东方Project-原曲" || pl.Creator != "鱼" || pl.Count != 3 {
		t.Fatalf("歌单信息映射错误: %+v", pl)
	}
	if pl.Cover == "" {
		t.Fatal("歌单封面为空")
	}
	if len(tracks) != 3 {
		t.Fatalf("应补齐 3 首，实际 %d", len(tracks))
	}
	if tracks[0].ID != "11" || tracks[1].ID != "22" || tracks[2].ID != "33" {
		t.Fatalf("曲目顺序应按 trackIds：%s,%s,%s", tracks[0].ID, tracks[1].ID, tracks[2].ID)
	}
	if tracks[0].Artist != "X/Y" || tracks[0].Duration != 245 || tracks[0].Cover == "" {
		t.Fatalf("曲目字段映射错误: %+v", tracks[0])
	}
	if tracks[1].Fee != 8 {
		t.Fatalf("fee 应取 detail privileges，实际 %d", tracks[1].Fee)
	}
	if detailCalls.Load() != 1 || songCalls.Load() != 1 {
		t.Fatalf("应各请求一次 detail/song：detail=%d song=%d", detailCalls.Load(), songCalls.Load())
	}
}

// TestPlaylistKeepsDetailTracks 验证详情已带全 tracks 时不再发 song/detail。
func TestPlaylistKeepsDetailTracks(t *testing.T) {
	var songCalls atomic.Int32
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/v3/playlist/detail"):
			_, _ = w.Write([]byte(`{"code":200,
				"playlist":{"id":1,"name":"小歌单","coverImgUrl":"https://p1.music.126.net/c.jpg",
					"creator":{"nickname":"n"},"trackCount":2,
					"trackIds":[{"id":11},{"id":22}],"tracks":[
						{"id":11,"name":"A","ar":[{"name":"X"}],"al":{"name":"AL","picUrl":"https://p1/x.jpg"},"dt":61000,"fee":0},
						{"id":22,"name":"B","ar":[{"name":"Y"}],"al":{"name":"AL","picUrl":"https://p1/x.jpg"},"dt":62000,"fee":8}]}}`))
		case strings.HasSuffix(r.URL.Path, "/v3/song/detail"):
			songCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"songs":[],"privileges":[]}`))
		default:
			http.NotFound(w, r)
		}
	})

	_, tracks, err := p.Playlist(context.Background(), "1")
	if err != nil {
		t.Fatalf("Playlist 失败: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("应返回详情中的 2 首，实际 %d", len(tracks))
	}
	if songCalls.Load() != 0 {
		t.Fatalf("详情已带全 tracks 时不应再请求 song/detail，实际 %d 次", songCalls.Load())
	}
}

// TestUserPlaylistsMapping 验证用户歌单映射（含 trackCount 缺失时用 trackIds 长度兜底）。
func TestUserPlaylistsMapping(t *testing.T) {
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/user/playlist") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"more":true,"playlist":[
			{"id":1,"name":"PL1","coverImgUrl":"https://p1/1.jpg","creator":{"nickname":"甲"},"trackCount":10},
			{"id":2,"name":"PL2","coverImgUrl":"","creator":{"nickname":"乙"},"trackCount":0,
			 "trackIds":[{"id":5},{"id":6}]}
		]}`))
	})

	playlists, err := p.UserPlaylists(context.Background(), "12345")
	if err != nil {
		t.Fatalf("UserPlaylists 失败: %v", err)
	}
	if len(playlists) != 2 {
		t.Fatalf("应返回 2 个歌单，实际 %d", len(playlists))
	}
	if playlists[0].ID != "1" || playlists[0].Name != "PL1" || playlists[0].Creator != "甲" ||
		playlists[0].Count != 10 || playlists[0].Cover == "" {
		t.Fatalf("第一个歌单映射错误: %+v", playlists[0])
	}
	if playlists[1].Count != 2 {
		t.Fatalf("trackCount 缺失时应退回 trackIds 长度 2，实际 %d", playlists[1].Count)
	}
}

// TestSearchPlaylistsCloudMapping 验证 cloudsearch type=1000 的歌单字段映射。
func TestSearchPlaylistsCloudMapping(t *testing.T) {
	var cloudCalls, legacyCalls atomic.Int32
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cloudsearch/get/web"):
			cloudCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"result":{"playlistCount":12,"playlists":[
				{"id":2416955681,"name":"东方Project-原曲","coverImgUrl":"https://p1.music.126.net/pc.jpg",
				 "creator":{"nickname":"被钓起来的鱼"},"trackCount":1398},
				{"id":406607901,"name":"东方纯音乐","coverImgUrl":"","creator":{"nickname":"乙"},"trackCount":0,
				 "trackIds":[{"id":1},{"id":2},{"id":3}]}
			]}}`))
		case strings.HasSuffix(r.URL.Path, "/search/get"):
			legacyCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"result":{"playlistCount":0,"playlists":[]}}`))
		default:
			http.NotFound(w, r)
		}
	})
	p.cfg.Cookie = "MUSIC_U=ok; __csrf=x"

	playlists, total, err := p.SearchPlaylists(context.Background(), "东方", 1, 5)
	if err != nil {
		t.Fatalf("SearchPlaylists 失败: %v", err)
	}
	if total != 12 || len(playlists) != 2 {
		t.Fatalf("total/len 不符：total=%d len=%d", total, len(playlists))
	}
	pl := playlists[0]
	if pl.ID != "2416955681" || pl.Name != "东方Project-原曲" ||
		pl.Creator != "被钓起来的鱼" || pl.Count != 1398 || pl.Cover == "" {
		t.Fatalf("歌单映射错误: %+v", pl)
	}
	if playlists[1].Count != 3 {
		t.Fatalf("trackCount 缺失时应退回 trackIds 长度 3，实际 %d", playlists[1].Count)
	}
	if cloudCalls.Load() != 1 || legacyCalls.Load() != 0 {
		t.Fatalf("有 cookie 时应只走 cloudsearch：cloud=%d legacy=%d", cloudCalls.Load(), legacyCalls.Load())
	}
}

// TestSearchPlaylistsCloudRejectedFallsBackToLegacy 验证 cookie 失效时歌单搜索降级到老接口。
func TestSearchPlaylistsCloudRejectedFallsBackToLegacy(t *testing.T) {
	var cloudCalls, legacyCalls atomic.Int32
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cloudsearch/get/web"):
			cloudCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":50000005}`))
		case strings.HasSuffix(r.URL.Path, "/search/get"):
			legacyCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"result":{"playlistCount":2,"playlists":[
				{"id":9,"name":"老接口歌单","coverImgUrl":"http://p1/9.jpg",
				 "creator":{"nickname":"丙"},"trackCount":42}
			]}}`))
		default:
			http.NotFound(w, r)
		}
	})
	p.cfg.Cookie = "MUSIC_U=expired; __csrf=x"

	playlists, total, err := p.SearchPlaylists(context.Background(), "东方", 1, 5)
	if err != nil {
		t.Fatalf("降级后 SearchPlaylists 仍失败: %v", err)
	}
	if total != 2 || len(playlists) != 1 {
		t.Fatalf("降级结果不符：total=%d len=%d", total, len(playlists))
	}
	if playlists[0].ID != "9" || playlists[0].Name != "老接口歌单" ||
		playlists[0].Creator != "丙" || playlists[0].Count != 42 {
		t.Fatalf("降级映射错误: %+v", playlists[0])
	}
	if playlists[0].Cover != "https://p1/9.jpg" {
		t.Fatalf("老接口 http 封面应强转 https，实际 %q", playlists[0].Cover)
	}
	if cloudCalls.Load() != 1 || legacyCalls.Load() != 1 {
		t.Fatalf("请求次数不符：cloud=%d legacy=%d", cloudCalls.Load(), legacyCalls.Load())
	}
}

// TestSearchPlaylistsAnonymousSkipsCloudSearch 验证无 cookie 时歌单搜索直接走老接口。
func TestSearchPlaylistsAnonymousSkipsCloudSearch(t *testing.T) {
	var cloudCalls, legacyCalls atomic.Int32
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cloudsearch/get/web"):
			cloudCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":50000005}`))
		case strings.HasSuffix(r.URL.Path, "/search/get"):
			legacyCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":200,"result":{"playlistCount":1,"playlists":[
				{"id":7,"name":"匿名歌单","coverImgUrl":"https://p1/7.jpg","creator":{"nickname":"丁"},"trackCount":8}
			]}}`))
		default:
			http.NotFound(w, r)
		}
	})

	playlists, total, err := p.SearchPlaylists(context.Background(), "东方", 1, 5)
	if err != nil || total != 1 || len(playlists) != 1 {
		t.Fatalf("匿名歌单搜索失败: err=%v total=%d len=%d", err, total, len(playlists))
	}
	if cloudCalls.Load() != 0 || legacyCalls.Load() != 1 {
		t.Fatalf("无 cookie 时不应请求 cloudsearch：cloud=%d legacy=%d", cloudCalls.Load(), legacyCalls.Load())
	}
}

// TestResolveFallsBackTo128kAndForcesHTTPS 验证 320k 空地址时退 128k，且 http 强转 https。
func TestResolveFallsBackTo128kAndForcesHTTPS(t *testing.T) {
	var calls atomic.Int32
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/song/enhance/player/url") {
			http.NotFound(w, r)
			return
		}
		if calls.Add(1) == 1 {
			// 第一次（320k）无地址：模拟无该码率版权。
			_, _ = w.Write([]byte(`{"code":200,"data":[{"id":1,"url":null,"br":0,"code":404}]}`))
			return
		}
		// 第二次（128k）返回 http 地址。
		_, _ = w.Write([]byte(`{"code":200,"data":[{"id":1,"url":"http://m10.music.126.net/a.mp3","br":128000,"code":200}]}`))
	})

	track := model.Track{ID: "1", Source: "wy", Name: "测试曲"}
	if err := p.Resolve(context.Background(), &track); err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if track.URL != "https://m10.music.126.net/a.mp3" {
		t.Fatalf("应强制 https，实际 %q", track.URL)
	}
	if calls.Load() != 2 {
		t.Fatalf("应依次请求 320k/128k 共 2 次，实际 %d", calls.Load())
	}
}

// TestResolveNoURLReturnsError 验证两个码率都无地址时返回清晰错误（调用方据此跳过）。
func TestResolveNoURLReturnsError(t *testing.T) {
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":[{"id":1,"url":null,"br":0,"code":404}]}`))
	})
	track := model.Track{ID: "1", Source: "wy", Name: "无版权曲", Fee: 1}
	err := p.Resolve(context.Background(), &track)
	if err == nil {
		t.Fatal("无播放地址时应返回 error")
	}
	if !strings.Contains(err.Error(), "无法解析播放地址") {
		t.Fatalf("错误信息应带上下文，实际: %v", err)
	}
	if track.URL != "" {
		t.Fatalf("失败时不应写入 URL，实际 %q", track.URL)
	}
}

// ---- 在线测试（需要真实网络；cookie 读取 config.local.json，读不到则跳过） ----

const liveTestTimeout = 90 * time.Second

// loadLiveCookie 从 config.local.json 读取 cookie；绝不打印 cookie 内容。
//
// 测试的工作目录是包目录 server/internal/provider/netease：
//   - 仓库根目录的 ../../../../config.local.json（任务给的路径）；
//   - 本仓库实际放在 server/ 下的 ../../../config.local.json。
func loadLiveCookie(t *testing.T) string {
	t.Helper()
	if c := strings.TrimSpace(os.Getenv("MUSIC_NETEASE_COOKIE")); c != "" {
		return c
	}
	for _, rel := range []string{"../../../../config.local.json", "../../../config.local.json"} {
		data, err := os.ReadFile(filepath.FromSlash(rel))
		if err != nil {
			continue
		}
		var cfg struct {
			NeteaseCookie string `json:"neteaseCookie"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Logf("解析 %s 失败: %v", rel, err)
			continue
		}
		if c := strings.TrimSpace(cfg.NeteaseCookie); c != "" {
			return c
		}
	}
	t.Skip("没有可用的网易云 cookie（config.local.json 缺失或 neteaseCookie 为空），跳过在线测试")
	return ""
}

func newLiveProvider(t *testing.T) *Provider {
	t.Helper()
	return New(Config{Cookie: loadLiveCookie(t), TimeoutSeconds: 15})
}

// TestLiveSearch 用真实 cookie 搜索「东方」，断言返回非空并打印映射结果。
func TestLiveSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过在线测试")
	}
	p := newLiveProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout)
	defer cancel()

	tracks, total, err := p.Search(ctx, "东方", 1, 5)
	if err != nil {
		if isNetworkUnavailable(err) {
			t.Skipf("网络不可用，跳过在线搜索测试: %v", err)
		}
		t.Fatalf("在线搜索失败: %v", err)
	}
	if total <= 0 {
		t.Fatalf("搜索「东方」total 应大于 0，实际 %d", total)
	}
	if len(tracks) == 0 {
		t.Fatalf("搜索「东方」返回 0 首曲目")
	}
	t.Logf("搜索「东方」: total=%d，本页返回 %d 首", total, len(tracks))
	for i, tr := range tracks {
		t.Logf("  #%d id=%s name=%q artist=%q album=%q duration=%ds fee=%d cover=%v",
			i+1, tr.ID, tr.Name, tr.Artist, tr.Album, tr.Duration, tr.Fee, tr.Cover != "")
		if tr.ID == "" || tr.Name == "" || tr.Source != "wy" || tr.Duration <= 0 {
			t.Fatalf("第 %d 首字段不完整: %+v", i+1, tr)
		}
	}
}

// TestLiveSearchAnonymous 验证无 cookie 时搜索不会 panic、走老接口降级仍能出结果。
func TestLiveSearchAnonymous(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过在线测试")
	}
	p := New(Config{TimeoutSeconds: 15})
	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout)
	defer cancel()

	tracks, total, err := p.Search(ctx, "东方", 1, 3)
	if err != nil {
		if isNetworkUnavailable(err) {
			t.Skipf("网络不可用，跳过匿名搜索测试: %v", err)
		}
		t.Fatalf("匿名搜索失败: %v", err)
	}
	if len(tracks) == 0 || total <= 0 {
		t.Fatalf("匿名搜索应返回非空结果，实际 total=%d len=%d", total, len(tracks))
	}
	t.Logf("匿名搜索「东方」: total=%d，本页返回 %d 首（老接口降级 + song/detail 补封面）", total, len(tracks))
	for i, tr := range tracks {
		if tr.ID == "" || tr.Name == "" {
			t.Fatalf("第 %d 首字段不完整: %+v", i+1, tr)
		}
		if i < 3 {
			t.Logf("  #%d id=%s name=%q artist=%q duration=%ds fee=%d cover=%v",
				i+1, tr.ID, tr.Name, tr.Artist, tr.Duration, tr.Fee, tr.Cover != "")
		}
	}
}

// TestLivePlaylistSearch 用真实 cookie 搜「东方」歌单，断言非空并打印映射结果。
func TestLivePlaylistSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过在线测试")
	}
	p := newLiveProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout)
	defer cancel()

	playlists, total, err := p.SearchPlaylists(ctx, "东方", 1, 5)
	if err != nil {
		if isNetworkUnavailable(err) {
			t.Skipf("网络不可用，跳过在线歌单搜索测试: %v", err)
		}
		t.Fatalf("在线歌单搜索失败: %v", err)
	}
	if total <= 0 || len(playlists) == 0 {
		t.Fatalf("歌单搜索「东方」应返回非空结果，实际 total=%d len=%d", total, len(playlists))
	}
	t.Logf("歌单搜索「东方」: total=%d，本页返回 %d 个", total, len(playlists))
	for i, pl := range playlists {
		t.Logf("  #%d id=%s name=%q creator=%q count=%d cover=%v",
			i+1, pl.ID, pl.Name, pl.Creator, pl.Count, pl.Cover != "")
		if pl.ID == "" || pl.Name == "" {
			t.Fatalf("第 %d 个歌单字段不完整: %+v", i+1, pl)
		}
	}
}

// TestLiveResolve 搜索结果第一首 Resolve：断言拿到 https URL；
// 版权/风控导致前几首解析失败时换下一首，全失败则 t.Skip 并打印原因。
func TestLiveResolve(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过在线测试")
	}
	p := newLiveProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout)
	defer cancel()

	tracks, _, err := p.Search(ctx, "东方", 1, 10)
	if err != nil {
		if isNetworkUnavailable(err) {
			t.Skipf("网络不可用，跳过 Resolve 测试: %v", err)
		}
		t.Fatalf("搜索候选失败: %v", err)
	}
	if len(tracks) == 0 {
		t.Fatalf("搜索候选为空")
	}

	var reasons []string
	var chosen *model.Track
	var chosenBr int
	for i := range tracks {
		tr := tracks[i]
		id, err := parseNumericID(tr.ID)
		if err != nil {
			reasons = append(reasons, tr.ID+": "+err.Error())
			continue
		}
		u, br, err := p.resolveURL(ctx, id)
		if err != nil {
			reasons = append(reasons, tr.ID+"《"+tr.Name+"》："+err.Error())
			continue
		}
		chosen, chosenBr = &tr, br
		host := ""
		if parsed, err := url.Parse(u); err == nil {
			host = parsed.Host
		}
		t.Logf("解析到播放地址: id=%s《%s》fee=%d host=%s br=%d", tr.ID, tr.Name, tr.Fee, host, br)
		break
	}
	if chosen == nil {
		t.Skipf("前 %d 首都无法解析播放地址（版权/风控限制），跳过: %s", len(tracks), strings.Join(reasons, "；"))
	}

	// 再用公开的 Resolve 走一遍，确认它确实把 URL 写回 Track 且强制成 https。
	if err := p.Resolve(ctx, chosen); err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if chosen.URL == "" {
		t.Fatal("Resolve 后 Track.URL 仍为空")
	}
	if !strings.HasPrefix(chosen.URL, "https://") {
		t.Fatalf("URL 必须强制 https，实际 %q", chosen.URL)
	}
	parsed, err := url.Parse(chosen.URL)
	if err != nil || parsed.Host == "" {
		t.Fatalf("URL 非法: %q (err=%v)", chosen.URL, err)
	}
	t.Logf("Resolve 成功: host=%s 首选码率=%d", parsed.Host, chosenBr)

	// 顺带验证歌词接口（失败不影响 Resolve 结论）。
	if lyric, err := p.Lyric(ctx, chosen.ID); err != nil {
		t.Logf("Lyric 失败（不致命）: %v", err)
	} else {
		t.Logf("Lyric 长度: %d 字节", len(lyric))
	}
}

// TestLivePlaylist 用真实接口取「东方Project-原曲」（架构文档里的默认电台歌单），
// 验证 trackIds 补齐路径：详情接口只回 20 首，必须靠 song/detail 补到 200 首。
func TestLivePlaylist(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过在线测试")
	}
	p := newLiveProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout)
	defer cancel()

	pl, tracks, err := p.Playlist(ctx, "2416955681")
	if err != nil {
		if isNetworkUnavailable(err) {
			t.Skipf("网络不可用，跳过歌单测试: %v", err)
		}
		t.Fatalf("在线取歌单失败: %v", err)
	}
	if pl.Name == "" || pl.Creator == "" || pl.Cover == "" || pl.Count < 1000 {
		t.Fatalf("歌单信息不完整: %+v", pl)
	}
	if len(tracks) < 100 {
		t.Fatalf("应按 trackIds 补齐至少 100 首，实际 %d", len(tracks))
	}
	t.Logf("歌单 %q（作者 %s，共 %d 首）取到 %d 首曲目", pl.Name, pl.Creator, pl.Count, len(tracks))
	for i := 0; i < 3 && i < len(tracks); i++ {
		tr := tracks[i]
		t.Logf("  #%d id=%s name=%q artist=%q album=%q duration=%ds fee=%d cover=%v",
			i+1, tr.ID, tr.Name, tr.Artist, tr.Album, tr.Duration, tr.Fee, tr.Cover != "")
	}
	for i, tr := range tracks {
		if tr.ID == "" || tr.Name == "" || tr.Source != "wy" {
			t.Fatalf("第 %d 首字段不完整: %+v", i+1, tr)
		}
	}
}

// isNetworkUnavailable 区分「本机没网」与「代码/接口错误」：只有前者才 Skip。
func isNetworkUnavailable(err error) bool {
	if err == nil {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, frag := range []string{
		"dial tcp", "no such host", "i/o timeout", "network is unreachable",
		"connection refused", "connection reset", "proxyconnect", "tls handshake timeout",
	} {
		if strings.Contains(msg, frag) {
			return true
		}
	}
	return false
}

func TestSearchUsersMapping(t *testing.T) {
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/search/get") {
			t.Error(r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":200,"result":{"userprofileCount":2,"userprofiles":[{"userId":123,"nickname":"听众","avatarUrl":"https://example.com/a.jpg"},{"userId":456,"nickname":"听众","avatarUrl":""}]}}`))
	})
	users, total, err := p.SearchUsers(context.Background(), "听众", 1, 10)
	if err != nil || total != 2 || len(users) != 2 || users[0].ID != "123" || users[1].ID != "456" || users[0].Nick != "听众" {
		t.Fatalf("%+v %d %v", users, total, err)
	}
	if _, _, err := p.SearchUsers(context.Background(), " ", 1, 10); err == nil {
		t.Fatal("empty nickname accepted")
	}
}

func TestResolveDoesNotProbeCDN(t *testing.T) {
	calls := 0
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"code":200,"data":[{"url":"https://unreachable.invalid/audio.mp3","br":320000}]}`))
	})
	track := model.Track{ID: "1", Source: "wy"}
	if err := p.Resolve(context.Background(), &track); err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
