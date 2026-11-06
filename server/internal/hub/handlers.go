package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hakrelin/music/server/internal/model"
)

// handle 分发客户端消息（docs §4.1）。未知类型回 error 而不是断开，
// 方便前端灰度上线新消息类型时老服务端还能正常用。
func (c *Client) handle(m *inMsg) {
	switch m.T {
	case "ping":
		c.sendJSON(pongOut{T: "pong", Now: c.hub.Now()})

	case "hello":
		// 重复 hello：连接已经建立，忽略。

	case "chat":
		c.handleChat(m)

	case "pick":
		t := model.Track{
			ID:       m.ID,
			Source:   m.Source,
			Name:     m.Name,
			Artist:   m.Artist,
			Album:    m.Album,
			Cover:    m.Cover,
			Duration: m.Duration,
			Fee:      m.Fee,
		}
		code, msg := c.room.pick(c, t)
		c.reply(code, msg, m.Req)

	case "search":
		c.handleSearch(m)

	case "playlist.search":
		c.handlePlaylistSearch(m)

	case "playlist.get":
		c.handlePlaylistGet(m)

	case "user.playlists":
		c.handleUserPlaylists(m)

	case "queue.remove":
		code, msg := c.room.removeTrack(c, m.ID)
		c.reply(code, msg, m.Req)

	case "queue.top":
		code, msg := c.room.topTrack(c, m.ID)
		c.reply(code, msg, m.Req)

	case "queue.clear":
		code, msg := c.room.clearQueue(c)
		c.reply(code, msg, m.Req)

	case "skip.vote":
		code, msg := c.room.voteSkip(c)
		c.reply(code, msg, m.Req)

	case "auth":
		c.handleAuth(m)

	case "radio.load":
		code, msg := c.room.loadRadio(c, m.Preset)
		c.reply(code, msg, m.Req)

	case "next":
		code, msg := c.room.next(c)
		c.reply(code, msg, m.Req)

	case "mode":
		code, msg := c.room.setMode(c, m.Loop, m.Random, m.Radio)
		c.reply(code, msg, m.Req)

	case "ban":
		code, msg := c.room.setBan(c, m.Choose, m.Switch)
		c.reply(code, msg, m.Req)

	case "goodmode":
		if m.On == nil {
			c.sendErrorCode("bad_request", "缺少 on", m.Req)
			return
		}
		code, msg := c.room.setGoodMode(c, *m.On)
		c.reply(code, msg, m.Req)

	case "volume":
		if m.Value == nil {
			c.sendErrorCode("bad_request", "缺少 value", m.Req)
			return
		}
		code, msg := c.room.setVolume(c, *m.Value)
		c.reply(code, msg, m.Req)

	case "good":
		code, msg := c.room.good(m.ID)
		c.reply(code, msg, m.Req)

	case "chat.black":
		c.handleBlacklist(m, true)

	case "chat.unblack":
		c.handleBlacklist(m, false)

	default:
		c.sendErrorCode("unknown_type", "未知消息类型："+m.T, m.Req)
	}
}

// reply 统一处理 (code, message) 形式的房间操作结果。
func (c *Client) reply(code, msg, req string) {
	if code == "" {
		return
	}
	c.sendErrorCode(code, msg, req)
}

func (c *Client) handleChat(m *inMsg) {
	code, msg := c.room.chat(c, m.Content)
	if code != "" {
		c.sendErrorCode(code, msg, m.Req)
	}
}

func (c *Client) handleAuth(m *inMsg) {
	role := strings.ToLower(strings.TrimSpace(m.Role))
	var want string
	switch role {
	case "root":
		want = c.hub.rootPassword
	case "admin":
		want = c.hub.adminPassword
	default:
		c.sendErrorCode("bad_request", "未知角色", m.Req)
		return
	}
	// 配置里没设口令时一律拒绝：绝不允许"空口令"直接拿到管理权限。
	if want == "" || m.Password != want {
		c.sendErrorCode("bad_password", "口令不对", m.Req)
		return
	}
	c.role = role
	c.sendJSON(authOut{
		T:       "auth",
		Role:    role,
		OK:      true,
		Message: "房主认证成功",
	})
	c.hub.log.Info("认证成功", "room", c.room.id, "session", c.session, "role", role)
}

func (c *Client) handleBlacklist(m *inMsg, black bool) {
	if !c.isAdmin() {
		c.sendErrorCode("forbidden", "没有权限", m.Req)
		return
	}
	session := strings.TrimSpace(m.Session)
	if session == "" {
		c.sendErrorCode("bad_request", "缺少 session", m.Req)
		return
	}
	code, msg := c.room.setBlacklist(session, black)
	c.reply(code, msg, m.Req)
	if black {
		c.room.notifySession(session, "你已被管理员禁言", "warn")
	} else {
		c.room.notifySession(session, "你已被解除禁言", "info")
	}
}

// sourceOf 解析消息里的 source：空则用主 provider，非法则报错。
func (c *Client) sourceOf(src string) (string, bool) {
	src = strings.TrimSpace(src)
	if src == "" {
		return c.hub.primary, true
	}
	if _, ok := c.hub.reg.For(src); ok {
		return src, true
	}
	return "", false
}

func (c *Client) paging(m *inMsg) (page, size int) {
	page = m.Page
	if page < 1 {
		page = 1
	}
	size = m.Size
	if size <= 0 {
		size = 10
	}
	if size > 30 {
		size = 30
	}
	return page, size
}

func (c *Client) handleSearch(m *inMsg) {
	source, ok := c.sourceOf(m.Source)
	if !ok {
		c.sendErrorCode("bad_source", "未知音乐源", m.Req)
		return
	}
	page, size := c.paging(m)
	tracks, total, err := c.hub.searchTracks(context.Background(), source, m.Q, page, size)
	if err != nil {
		c.sendErrorCode("provider_error", "搜索失败："+err.Error(), m.Req)
		return
	}
	if tracks == nil {
		tracks = []model.Track{}
	}
	c.sendJSON(searchOut{T: "search", Req: m.Req, Tracks: tracks, Total: total})
}

func (c *Client) handlePlaylistSearch(m *inMsg) {
	source, ok := c.sourceOf(m.Source)
	if !ok {
		c.sendErrorCode("bad_source", "未知音乐源", m.Req)
		return
	}
	page, size := c.paging(m)
	playlists, total, err := c.hub.playlistSearch(context.Background(), source, m.Q, page, size)
	if err != nil {
		c.sendErrorCode("provider_error", "歌单搜索失败："+err.Error(), m.Req)
		return
	}
	if playlists == nil {
		playlists = []model.Playlist{}
	}
	c.sendJSON(playlistSearchOut{T: "playlist.search", Req: m.Req, Playlists: playlists, Total: total})
}

func (c *Client) handlePlaylistGet(m *inMsg) {
	source, ok := c.sourceOf(m.Source)
	if !ok {
		c.sendErrorCode("bad_source", "未知音乐源", m.Req)
		return
	}
	id := strings.TrimSpace(m.ID)
	if id == "" {
		c.sendErrorCode("bad_request", "歌单 ID 不能为空", m.Req)
		return
	}
	pl, tracks, err := c.hub.playlistGet(context.Background(), source, id)
	if err != nil {
		c.sendErrorCode("provider_error", "歌单读取失败："+err.Error(), m.Req)
		return
	}
	if tracks == nil {
		tracks = []model.Track{}
	}
	c.sendJSON(playlistGetOut{T: "playlist.get", Req: m.Req, Playlist: pl, Tracks: tracks})
}

func (c *Client) handleUserPlaylists(m *inMsg) {
	source, ok := c.sourceOf(m.Source)
	if !ok {
		c.sendErrorCode("bad_source", "未知音乐源", m.Req)
		return
	}
	uid := strings.TrimSpace(m.UID)
	if uid == "" {
		c.sendErrorCode("bad_request", "用户 ID 不能为空", m.Req)
		return
	}
	playlists, err := c.hub.userPlaylists(context.Background(), source, uid)
	if err != nil {
		c.sendErrorCode("provider_error", "用户歌单读取失败："+err.Error(), m.Req)
		return
	}
	if playlists == nil {
		playlists = []model.Playlist{}
	}
	c.sendJSON(userPlaylistsOut{T: "user.playlists", Req: m.Req, Playlists: playlists})
}

// writeHTTPError 输出 docs §3 约定的错误 JSON。
func writeHTTPError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
