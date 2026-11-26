package httpapi

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hakrelin/music/server/internal/stickers"
)

const pngOnePixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

// 用最小 Server（只挂表情包路由）跑一遍真实的 HTTP 往返：
// 公开列表 / 上传 / 取图（含长缓存头）/ 404 / 非图片类型 / 未登录删除被拦。
func TestStickerHTTPRoundTrip(t *testing.T) {
	store, err := stickers.New(t.TempDir())
	if err != nil {
		t.Fatalf("stickers.New: %v", err)
	}
	srv := &Server{
		stickers: store,
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	png, err := base64.StdEncoding.DecodeString(pngOnePixel)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stickers", srv.handleStickerList)
	mux.HandleFunc("GET /api/stickers/{id}", srv.handleStickerFile)
	mux.HandleFunc("POST /api/stickers", srv.handleStickerUpload)
	mux.HandleFunc("DELETE /api/stickers/{id}", srv.requireAdmin(srv.handleStickerDelete))

	// 1) 空库返回 []，不能是 null（前端直接 .length / .map）
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/stickers", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("空列表 = %d %s", rec.Code, rec.Body.String())
	}

	// 2) 上传裸图片字节（前端用 File 当 body 的那条路径）
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/stickers?name=doge.png", strings.NewReader(string(png)))
	req.Header.Set("Content-Type", "image/png")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("上传 = %d %s", rec.Code, rec.Body.String())
	}
	items := store.List()
	if len(items) != 1 {
		t.Fatalf("上传后条数 = %d", len(items))
	}

	// 3) 取图：类型、长度、不可变缓存头都对
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/stickers/"+items[0].ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("取图 = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	if rec.Body.Len() != len(png) {
		t.Fatalf("响应长度 = %d, want %d", rec.Body.Len(), len(png))
	}

	// 4) 不存在的 ID
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/stickers/deadbeefdeadbeef", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在 ID = %d", rec.Code)
	}

	// 5) 非图片类型
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/stickers", strings.NewReader("not an image"))
	req.Header.Set("Content-Type", "text/plain")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("非图片 = %d", rec.Code)
	}

	// 6) 未登录删除必须被挡（否则任何人都能清空别人的表情库）
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/stickers/"+items[0].ID, nil))
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Fatalf("未登录删除 = %d，应当 401/403", rec.Code)
	}
	if len(store.List()) != 1 {
		t.Fatal("未登录删除居然生效了")
	}
}
