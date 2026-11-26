package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hakrelin/music/server/internal/stickers"
)

// handleStickerList 返回表情包列表。
//
// 公开可读：表情包要在聊天面板里展示，房间里任何人都得看到缩略图，
// 但它只是静态图片，不含任何服务端状态，所以不要求鉴权。
func (s *Server) handleStickerList(w http.ResponseWriter, _ *http.Request) {
	if s.stickers == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []stickers.Item{}})
		return
	}
	items := s.stickers.List()
	if items == nil {
		items = []stickers.Item{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleStickerFile 直接吐出表情包字节。
//
// ID 是内容寻址（每次导入都新生成），所以可以给长缓存：浏览器和 CDN 都不会重复请求。
func (s *Server) handleStickerFile(w http.ResponseWriter, r *http.Request) {
	if s.stickers == nil {
		writeError(w, http.StatusNotFound, "not_found", "表情包功能未启用")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	item, ok := s.stickers.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "表情包不存在")
		return
	}
	_, f, err := s.stickers.Open(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "表情包文件缺失")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", item.MIME)
	w.Header().Set("Content-Length", strconv.FormatInt(item.Size, 10))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, item.ID, time.Unix(item.Created, 0), f)
}

// handleStickerUpload 导入表情包。
//
// 支持两种请求体：
//   - 裸图片字节 + Content-Type: image/*（前端 fetch 直接传 File；
//   - multipart/form-data，字段名 file（方便 curl -F 或第三方工具）。
//
// 上限 2MB，实际类型以服务端嗅探为准，不信任 Content-Type。
func (s *Server) handleStickerUpload(w http.ResponseWriter, r *http.Request) {
	if s.stickers == nil {
		writeError(w, http.StatusServiceUnavailable, "disabled", "表情包功能未启用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, stickers.MaxItemBytes+1024)

	var (
		data []byte
		name string
		err  error
	)
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		if err := r.ParseMultipartForm(stickers.MaxItemBytes + 1024); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "表单解析失败或文件过大")
			return
		}
		file, header, ferr := r.FormFile("file")
		if ferr != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "缺少 file 字段")
			return
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, stickers.MaxItemBytes+1))
		if header != nil {
			name = header.Filename
		}
	} else {
		data, err = io.ReadAll(io.LimitReader(r.Body, stickers.MaxItemBytes+1))
		name = r.URL.Query().Get("name")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "读取上传内容失败")
		return
	}
	if len(data) > stickers.MaxItemBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", stickers.ErrTooLarge.Error())
		return
	}

	item, err := s.stickers.Add(name, data)
	switch {
	case errors.Is(err, stickers.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", err.Error())
		return
	case errors.Is(err, stickers.ErrUnsupported):
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_type", err.Error())
		return
	case errors.Is(err, stickers.ErrFull):
		writeError(w, http.StatusConflict, "full", err.Error())
		return
	case err != nil:
		s.log.Error("导入表情包失败", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "保存表情包失败")
		return
	}
	s.log.Info("表情包已导入", "id", item.ID, "name", item.Name, "size", item.Size, "mime", item.MIME)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item, "url": item.URL()})
}

// handleStickerDelete 删除表情包，仅管理员（rootPassword 会话）可用——
// 避免任何人把别人导入的表情清空。
func (s *Server) handleStickerDelete(w http.ResponseWriter, r *http.Request) {
	if s.stickers == nil {
		writeError(w, http.StatusServiceUnavailable, "disabled", "表情包功能未启用")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if err := s.stickers.Delete(id); err != nil {
		switch {
		case errors.Is(err, stickers.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		case errors.Is(err, stickers.ErrBuiltin):
			writeError(w, http.StatusConflict, "builtin", err.Error())
			return
		}
		s.log.Error("删除表情包失败", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "删除表情包失败")
		return
	}
	s.log.Info("表情包已删除", "id", id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

