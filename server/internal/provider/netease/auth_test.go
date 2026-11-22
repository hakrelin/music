package netease

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestQRLoginAndAccountStatus(t *testing.T) {
	pollStep := 0
	p := newMockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/login/qrcode/unikey"):
			_, _ = w.Write([]byte(`{"code":200,"unikey":"wy-key-123"}`))
		case strings.HasSuffix(r.URL.Path, "/login/qrcode/client/login"):
			pollStep++
			switch pollStep {
			case 1:
				_, _ = w.Write([]byte(`{"code":801,"message":"等待扫码"}`))
			case 2:
				_, _ = w.Write([]byte(`{"code":802,"message":"待确认","nickname":"博丽灵梦","avatarUrl":"http://p1.music.126.net/a.jpg"}`))
			case 3:
				http.SetCookie(w, &http.Cookie{Name: "MUSIC_U", Value: "token-xyz", Path: "/"})
				http.SetCookie(w, &http.Cookie{Name: "__csrf", Value: "csrf-999", Path: "/"})
				_, _ = w.Write([]byte(`{"code":803,"message":"授权登陆成功"}`))
			default:
				_, _ = w.Write([]byte(`{"code":800,"message":"二维码不存在或已过期"}`))
			}
		case strings.HasSuffix(r.URL.Path, "/w/nuser/account/get"):
			if !strings.Contains(r.Header.Get("Cookie"), "MUSIC_U=token-xyz") {
				_, _ = w.Write([]byte(`{"code":200,"account":null,"profile":null}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"account":{"id":10086,"vipType":11},"profile":{"userId":10086,"nickname":"博丽灵梦","avatarUrl":"http://p1.music.126.net/a.jpg","vipType":11}}`))
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()
	st0, err := p.AccountStatus(ctx)
	if err != nil || st0.Configured || st0.LoggedIn {
		t.Fatalf("empty cookie status mismatch: %+v err=%v", st0, err)
	}

	qr, err := p.CreateLoginQR(ctx)
	if err != nil {
		t.Fatalf("CreateLoginQR failed: %v", err)
	}
	if qr.Key != "wy-key-123" || !strings.Contains(qr.URL, "codekey=wy-key-123") {
		t.Fatalf("unexpected QR: %+v", qr)
	}

	r1, err := p.PollLoginQR(ctx, qr.Key)
	if err != nil || r1.Status != "waiting" {
		t.Fatalf("step1 want waiting, got %+v err=%v", r1, err)
	}
	r2, err := p.PollLoginQR(ctx, qr.Key)
	if err != nil || r2.Status != "scanned" || r2.Nickname != "博丽灵梦" || r2.Avatar != "https://p1.music.126.net/a.jpg" {
		t.Fatalf("step2 mismatch: %+v err=%v", r2, err)
	}
	r3, err := p.PollLoginQR(ctx, qr.Key)
	if err != nil || r3.Status != "confirmed" || !strings.Contains(r3.Cookie, "MUSIC_U=token-xyz") || !strings.Contains(r3.Cookie, "__csrf=csrf-999") {
		t.Fatalf("step3 mismatch: %+v err=%v", r3, err)
	}

	p.SetCookie(r3.Cookie)
	st1, err := p.AccountStatus(ctx)
	if err != nil || !st1.LoggedIn || st1.UID != "10086" || st1.Nickname != "博丽灵梦" || st1.VIPType != 11 {
		t.Fatalf("logged in status mismatch: %+v err=%v", st1, err)
	}

	r4, err := p.PollLoginQR(ctx, qr.Key)
	if err != nil || r4.Status != "expired" {
		t.Fatalf("step4 want expired, got %+v err=%v", r4, err)
	}
}
