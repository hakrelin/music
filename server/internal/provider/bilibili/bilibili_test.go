package bilibili

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWBISignatureKnownVector(t *testing.T) {
	// 社区公开 WBI 参考向量：验证 mixinKey 重排与 %20 空格编码。
	imgKey := "7cd084941338484aae1ad9425b84077c"
	subKey := "4932caff0ff746eab6f01bf08b70ac45"
	mixin := getMixinKey(imgKey, subKey)
	wantMixin := "ea1db124af3c7062474693fa704f4ff8"
	if mixin != wantMixin {
		t.Fatalf("mixinKey = %q, want %q", mixin, wantMixin)
	}

	params := url.Values{}
	params.Set("keyword", "Bad Apple!")
	params.Set("search_type", "video")
	qs := signWBIQuery(params, mixin, time.Unix(1702204169, 0))
	if !strings.Contains(qs, "keyword=Bad%20Apple&") {
		t.Fatalf("WBI query should strip '!' and encode space as %%20, got %q", qs)
	}
	if !strings.Contains(qs, "&w_rid=") {
		t.Fatalf("missing w_rid in %q", qs)
	}
}

func TestDefaultLoginStateEnforcement(t *testing.T) {
	p := New(Config{UseLogin: true, Cookie: ""})
	if _, _, err := p.Search(context.Background(), "东方", 1, 10); err == nil {
		t.Fatal("default useLogin=true with empty cookie should fail")
	}
	p.UpdateConfig(Config{UseLogin: true, Cookie: "buvid3=only-visitor"})
	if _, _, err := p.Search(context.Background(), "东方", 1, 10); err == nil {
		t.Fatal("cookie without SESSDATA should fail when useLogin=true")
	}
}

func TestSearchResolveAudioOnlyAndRangeProxy(t *testing.T) {
	audioPayload := []byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			_, _ = w.Write([]byte(`{"code":0,"data":{"isLogin":true,"mid":777,"uname":"魔理沙","face":"//i0.hdslb.com/face.jpg","vipStatus":1,"vip_label":{"text":"大会员"},"wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png","sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"}}}`))
		case "/x/frontend/finger/spi":
			_, _ = w.Write([]byte(`{"code":0,"data":{"b_3":"B3-TEST","b_4":"B4-TEST"}}`))
		case "/x/web-interface/wbi/search/type":
			if !strings.Contains(r.Header.Get("Cookie"), "SESSDATA=valid-sess") || !strings.Contains(r.Header.Get("Cookie"), "buvid3=B3-TEST") {
				t.Errorf("missing login or buvid3 cookie: %q", r.Header.Get("Cookie"))
			}
			if r.URL.Query().Get("w_rid") == "" || r.URL.Query().Get("wts") == "" {
				t.Errorf("missing WBI signature: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"numResults":1,"result":[{"bvid":"BV1xx411c7mD","aid":12,"title":"【东方】<em class=\"keyword\">Bad Apple</em>!! &amp; Vocal","author":"幻想乡UP","pic":"//i0.hdslb.com/cover.jpg","duration":"3:39","typename":"音乐综合"}]}}`))
		case "/x/web-interface/view":
			_, _ = w.Write([]byte(`{"code":0,"data":{"bvid":"BV1xx411c7mD","aid":12,"title":"Bad Apple Full","pic":"http://i0.hdslb.com/v.jpg","duration":219,"cid":9001,"owner":{"name":"幻想乡UP"},"pages":[{"cid":9001,"page":1,"part":"原版","duration":219},{"cid":9002,"page":2,"part":"纯音乐","duration":200}]}}`))
		case "/x/player/wbi/playurl":
			if r.URL.Query().Get("fnval") != "16" {
				t.Errorf("expected fnval=16 for DASH audio, got %q", r.URL.Query().Get("fnval"))
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"dash":{"video":[{"id":80,"baseUrl":"` + srvURL + `/cdn/video.m4s"}],"audio":[` +
				`{"id":30216,"baseUrl":"` + srvURL + `/cdn/audio64.m4s","bandwidth":64000,"codecs":"mp4a.40.2"},` +
				`{"id":30280,"baseUrl":"` + srvURL + `/cdn/audio192-bad.m4s","backupUrl":["` + srvURL + `/cdn/audio192-ok.m4s"],"bandwidth":192000,"codecs":"mp4a.40.2"},` +
				`{"id":30232,"baseUrl":"` + srvURL + `/cdn/audio132.m4s","bandwidth":132000,"codecs":"mp4a.40.2"}` +
				`]}}}`))
		case "/cdn/audio192-bad.m4s":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>unexpected response</html>"))
		case "/cdn/audio192-ok.m4s", "/cdn/audio132.m4s":
			if r.Header.Get("Referer") != "https://www.bilibili.com/" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Accept-Ranges", "bytes")
			if r.Header.Get("Range") == "bytes=100-" {
				w.Header().Set("Content-Range", "bytes */62")
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			if r.Header.Get("Range") == "bytes=0-9" {
				w.Header().Set("Content-Range", "bytes 0-9/62")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(audioPayload[:10])
				return
			}
			_, _ = w.Write(audioPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	p := New(Config{Cookie: "SESSDATA=valid-sess; bili_jct=csrf1", UseLogin: true, TimeoutSeconds: 5})
	p.apiBase = srv.URL

	ctx := context.Background()
	tracks, total, err := p.Search(ctx, "Bad Apple", 1, 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if total != 1 || len(tracks) != 1 {
		t.Fatalf("unexpected search count: total=%d len=%d", total, len(tracks))
	}
	tr := tracks[0]
	if tr.ID != "BV1xx411c7mD" || tr.Source != "bili" || tr.Name != "【东方】Bad Apple!! & Vocal" || tr.Duration != 219 || tr.Cover != "https://i0.hdslb.com/cover.jpg" {
		t.Fatalf("track mapping mismatch: %+v", tr)
	}

	// 直接按 BV 链接搜出多 P 分集
	parts, ptotal, err := p.Search(ctx, "https://www.bilibili.com/video/BV1xx411c7mD?p=2", 1, 10)
	if err != nil || ptotal != 2 || len(parts) != 2 || parts[0].ID != "BV1xx411c7mD_p2" {
		t.Fatalf("direct BV multi-P search mismatch: err=%v total=%d parts=%+v", err, ptotal, parts)
	}

	if _, _, err := p.Search(ctx, "BV1xx411c7mD_p3", 1, 10); err == nil {
		t.Fatal("a nonexistent part must not silently fall back to P1")
	}
	late, total, err := p.Search(ctx, "BV1xx411c7mD_p2", int(^uint(0)>>1), 30)
	if err != nil || len(late) != 0 || total != 2 {
		t.Fatalf("out-of-range search page: len=%d total=%d err=%v", len(late), total, err)
	}

	// Resolve 仅抽音频并返回 /api/stream/{id}?source=bili
	if err := p.Resolve(ctx, &tr); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if tr.URL != "/api/stream/BV1xx411c7mD?source=bili" {
		t.Fatalf("unexpected resolved URL: %q", tr.URL)
	}

	// lower=true 应选 132k 音频轨道（用于缓存/降码率）
	lowURLs, err := p.ResolveUpstreamAudio(ctx, "BV1xx411c7mD", true)
	if err != nil || len(lowURLs) == 0 || !strings.HasSuffix(lowURLs[0], "/cdn/audio132.m4s") {
		t.Fatalf("lower=true should pick 30232 (132k), got %q err=%v", lowURLs, err)
	}

	// ServeAudio 应自动从 403 主地址回退到 backupUrl，并正确映射 Content-Type: audio/mp4 与 206 Range
	req := httptest.NewRequest(http.MethodGet, "/api/stream/BV1xx411c7mD?source=bili", nil)
	req.Header.Set("Range", "bytes=0-9")
	rec := httptest.NewRecorder()
	if !p.ServeAudio(rec, req, "BV1xx411c7mD", false) {
		t.Fatal("ServeAudio should succeed via backupUrl")
	}
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "audio/mp4" {
		t.Fatalf("expected Content-Type audio/mp4, got %q", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "0123456789" {
		t.Fatalf("unexpected range body: %q", rec.Body.String())
	}

	badRange := httptest.NewRequest(http.MethodGet, "/api/stream/BV1xx411c7mD?source=bili", nil)
	badRange.Header.Set("Range", "bytes=100-")
	rangeResp := httptest.NewRecorder()
	if !p.ServeAudio(rangeResp, badRange, "BV1xx411c7mD", false) ||
		rangeResp.Code != http.StatusRequestedRangeNotSatisfiable ||
		rangeResp.Header().Get("Content-Range") != "bytes */62" {
		t.Fatalf("unexpected invalid range response: %d %v", rangeResp.Code, rangeResp.Header())
	}

	// 账号登录态校验
	st, err := p.AccountStatus(ctx)
	if err != nil || !st.LoggedIn || st.UID != "777" || st.Username != "魔理沙" || st.VIPLabel != "大会员" {
		t.Fatalf("AccountStatus mismatch: %+v err=%v", st, err)
	}
}

func TestBiliQRLoginFlow(t *testing.T) {
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/passport-login/web/qrcode/generate":
			_, _ = w.Write([]byte(`{"code":0,"data":{"url":"https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=bili-qr-key","qrcode_key":"bili-qr-key"}}`))
		case "/x/passport-login/web/qrcode/poll":
			step++
			switch step {
			case 1:
				_, _ = w.Write([]byte(`{"code":0,"data":{"code":86101,"message":"未扫码"}}`))
			case 2:
				_, _ = w.Write([]byte(`{"code":0,"data":{"code":86090,"message":"二维码已扫码未确认"}}`))
			case 3:
				http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "qr-sess-123"})
				http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: "qr-jct-456"})
				http.SetCookie(w, &http.Cookie{Name: "DedeUserID", Value: "8888"})
				_, _ = w.Write([]byte(`{"code":0,"data":{"code":0,"message":""}}`))
			default:
				_, _ = w.Write([]byte(`{"code":0,"data":{"code":86038,"message":"二维码已失效"}}`))
			}
		case "/x/frontend/finger/spi":
			_, _ = w.Write([]byte(`{"code":0,"data":{"b_3":"SPI-B3","b_4":"SPI-B4"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := New(Config{UseLogin: true})
	p.apiBase = srv.URL
	p.passportBase = srv.URL
	ctx := context.Background()

	qr, err := p.CreateLoginQR(ctx)
	if err != nil || qr.Key != "bili-qr-key" {
		t.Fatalf("CreateLoginQR failed: %+v err=%v", qr, err)
	}
	if r1, err := p.PollLoginQR(ctx, qr.Key); err != nil || r1.Status != "waiting" {
		t.Fatalf("step1 want waiting, got %+v err=%v", r1, err)
	}
	if r2, err := p.PollLoginQR(ctx, qr.Key); err != nil || r2.Status != "scanned" {
		t.Fatalf("step2 want scanned, got %+v err=%v", r2, err)
	}
	r3, err := p.PollLoginQR(ctx, qr.Key)
	if err != nil || r3.Status != "confirmed" ||
		!strings.Contains(r3.Cookie, "SESSDATA=qr-sess-123") ||
		!strings.Contains(r3.Cookie, "bili_jct=qr-jct-456") ||
		!strings.Contains(r3.Cookie, "buvid3=SPI-B3") {
		t.Fatalf("step3 confirmed cookie mismatch: %+v err=%v", r3, err)
	}
	if r4, err := p.PollLoginQR(ctx, qr.Key); err != nil || r4.Status != "expired" {
		t.Fatalf("step4 want expired, got %+v err=%v", r4, err)
	}
}

func TestSelectAudioURLsSkipsNonAAC(t *testing.T) {
	urls := selectAudioURLs([]dashAudioItem{
		{ID: 30280, Codecs: "ec-3", BaseURL: "https://example.invalid/dolby"},
		{ID: 30250, Codecs: "fLaC", BaseURL: "https://example.invalid/flac"},
		{ID: 30216, Codecs: "mp4a.40.2", BaseURL: "https://example.invalid/aac"},
	}, false)
	if len(urls) != 1 || urls[0] != "https://example.invalid/aac" {
		t.Fatalf("unexpected audio candidates: %v", urls)
	}
}
