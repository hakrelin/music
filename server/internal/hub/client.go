package hub

import (
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

// WS 相关的时间参数：
//   - helloWait：升级后必须在 10s 内发 hello（docs §4）；
//   - readWait：客户端 20s 一次 ping，服务端 25s 一次 control ping，
//     所以 75s 没任何帧就认为连接已死；
//   - writeWait：单帧写超时。
const (
	helloWait = 10 * time.Second
	readWait  = 75 * time.Second
	writeWait = 10 * time.Second
	pingEvery = 25 * time.Second
	sendBuf   = 256
)

var errRoomGone = errors.New("房间不存在")
var errBadPassword = errors.New("房间口令不对")

// upgrader 不校验 Origin：
// 本站正常部署在同源反代后（nginx），开发时前端由 Vite 代理访问；
// 额外允许任意 Origin 是为了方便用手机/局域网 IP 直接连，v1 只读数据无 CSRF 风险。
var upgrader = websocket.Upgrader{
	ReadBufferSize:   4096,
	WriteBufferSize:  4096,
	HandshakeTimeout: 8 * time.Second,
	CheckOrigin:      func(*http.Request) bool { return true },
}

// Client 是一条 WS 会话。
type Client struct {
	hub  *Hub
	room *Room
	conn *websocket.Conn

	session string
	nick    string
	role    string // user | admin | root
	remote  string

	send chan []byte
	done chan struct{}

	closeOnce sync.Once
	joined    bool // 只在持房间锁时读写
}

func (c *Client) isAdmin() bool { return c.role == "admin" || c.role == "root" }

// enqueue 把一帧放进发送缓冲；缓冲满说明客户端太慢，直接断开，
// 避免一个慢连接拖住整个房间的广播。
func (c *Client) enqueue(payload []byte) {
	select {
	case c.send <- payload:
	default:
		c.close()
	}
}

func (c *Client) sendJSON(v any) { c.enqueue(mustJSON(v)) }

func (c *Client) sendErrorCode(code, message, req string) {
	c.sendJSON(errorOut{T: "error", Code: code, Message: message, Req: req})
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// ServeWS 处理 GET /api/ws?room=...&nick=...。
//
// 握手顺序：HTTP 升级 → 等 hello（10s）→ 校验房间密码 → 发
// hello/state/snapshot → 广播 presence → 进入读写泵。docs §4.2 要求
// 客户端按 hello → state → snapshot 的顺序收到前三条消息。
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	roomID := strings.TrimSpace(r.URL.Query().Get("room"))
	queryNick := r.URL.Query().Get("nick")

	room, ok := h.Room(roomID)
	if !ok {
		writeHTTPError(w, http.StatusNotFound, "room_not_found", "房间不存在")
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade 失败时已经写过响应了，只记日志。
		h.log.Warn("WS 升级失败", "room", roomID, "err", err)
		return
	}

	c := &Client{
		hub:     h,
		room:    room,
		conn:    conn,
		session: newSessionID(),
		nick:    sanitizeNick(queryNick),
		role:    "user",
		remote:  r.RemoteAddr,
		send:    make(chan []byte, sendBuf),
		done:    make(chan struct{}),
	}

	// 1) 握手：等 hello，超时即断开
	_ = conn.SetReadDeadline(time.Now().Add(helloWait))
	var first inMsg
	if err := conn.ReadJSON(&first); err != nil {
		_ = conn.Close()
		return
	}
	if first.T != "hello" {
		c.writeDirect(errorOut{T: "error", Code: "bad_request", Message: "首条消息必须是 hello"})
		_ = conn.Close()
		return
	}
	if n := sanitizeNick(first.Nick); n != "" {
		c.nick = n
	}
	if c.nick == "" {
		c.nick = fmt.Sprintf("游客%04d", 1000+rand.IntN(9000))
	}

	// 2) 入房（密码校验 + 昵称去重）
	if err := room.join(c, first.Pwd, c.nick); err != nil {
		code, msg := "bad_password", "房间口令不对"
		if errors.Is(err, errRoomGone) {
			code, msg = "room_not_found", "房间不存在"
		}
		c.writeDirect(errorOut{T: "error", Code: code, Message: msg})
		_ = conn.Close()
		return
	}

	// 3) 前三条消息
	room.mu.Lock()
	hello := room.helloPayloadLocked(c)
	state := room.statePayloadLocked()
	snapshot := room.snapshotPayloadLocked()
	room.mu.Unlock()
	c.enqueue(hello)
	c.enqueue(state)
	c.enqueue(snapshot)

	_ = conn.SetReadDeadline(time.Time{})
	go c.writePump()
	go c.readPump()

	// 4) 通知其他人（新客户端已经拿到 snapshot，此时再发 presence 不会乱序）
	room.broadcastPresence()
	room.broadcastQueue()
	h.log.Info("WS 已连接", "room", room.id, "session", c.session, "nick", c.nick, "remote", c.remote)

	// 如果房间还没开始播放（比如刚启动的电台房），顺手催一下。
	go room.start()
}

// writeDirect 在 writePump 启动前直接写一帧（握手失败路径）。
func (c *Client) writeDirect(v any) {
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	_ = c.conn.WriteJSON(v)
}

// readPump 读客户端消息并分发；任何读错误都结束会话。
func (c *Client) readPump() {
	defer c.close()
	_ = c.conn.SetReadDeadline(time.Now().Add(readWait))
	c.conn.SetReadLimit(64 << 10)
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(readWait))
	})
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(readWait))
		var msg inMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			c.sendErrorCode("bad_request", "消息不是合法 JSON", "")
			continue
		}
		c.handle(&msg)
	}
}

// writePump 独占写端；退出时把会话从房间移出。
func (c *Client) writePump() {
	ticker := time.NewTicker(pingEvery)
	defer func() {
		ticker.Stop()
		c.close()
		c.hub.leave(c)
	}()
	for {
		select {
		case payload := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

// leave 从房间移除连接并广播在线人数（由 writePump 的 defer 调用）。
func (h *Hub) leave(c *Client) {
	c.room.leave(c)
	h.log.Info("WS 已断开", "room", c.room.id, "session", c.session, "nick", c.nick)
}

// sanitizeNick 裁剪昵称：去首尾空白、去掉控制字符、最多 16 字。
func sanitizeNick(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > 16 {
		s = string([]rune(s)[:16])
	}
	return s
}

// newSessionID 生成会话 ID（docs §2 的 session 字段，形如 s_ab12cd34）。
func newSessionID() string {
	b := make([]byte, 4)
	if _, err := crand.Read(b); err != nil {
		return fmt.Sprintf("s_%08x", rand.Uint32())
	}
	return "s_" + hex.EncodeToString(b)
}
