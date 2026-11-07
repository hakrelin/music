// Package hub 是房间管理器：房间状态机、WS 会话、服务器时钟推进、
// 电台 DJ、聊天、权限与投票都在这里。
//
// 并发模型：
//   - Hub.mu 保护房间表（增删查），锁序固定为 Hub.mu → Room.mu。
//   - Room.mu 保护单个房间的全部可变态（队列、在线、模式、聊天……）。
//   - 广播在持锁时只做"序列化 + 塞进各连接的缓冲 channel"，真正的网络写
//     在每连接自己的 writePump 里，持锁期间不做任何 IO/网络请求。
//   - 需要调 provider（搜索、解析 URL、拉歌单）的地方一律先释放房间锁，
//     拿完结果再加锁校验状态后提交。
package hub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hakrelin/music/server/internal/config"
	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider"
)

const (
	// 电台房补歌参数，见 docs §1：不足 3 首补到 8 首。
	radioMinQueue    = 3
	radioTargetQueue = 8

	// 聊天历史保留条数（snapshot.chat）。
	chatHistoryLimit = 100

	// 点歌队列上限：防止单连接刷爆内存。
	queueLimit = 500

	// 空房回收检查间隔 / 播放推进 tick。
	tickInterval = 500 * time.Millisecond
)

// Preset 是 /api/config 里的 radioPresets 条目，也是 radio.load 的 preset id。
type Preset struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Playlists []string `json:"playlists"`
}

// Options 是构造 Hub 所需的全部依赖与策略。
type Options struct {
	Version         string
	Registry        *provider.Registry
	PrimaryProvider string
	RootPassword    string
	AdminPassword   string
	VoteSkipRatio   float64
	IdleTimeout     time.Duration
	RadioRooms      []config.RadioRoom
	Logger          *slog.Logger
	Now             func() time.Time // 测试注入；nil 用 time.Now
}

// Hub 管理所有房间。
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]*Room

	reg     *provider.Registry
	primary string
	urls    *urlCache
	presets []Preset
	log     *slog.Logger
	nowFn   func() time.Time

	rootPassword  string
	adminPassword string
	voteRatio     float64
	idleTimeout   time.Duration

	radioRooms []config.RadioRoom

	closed bool
}

// New 构造 Hub。内置电台房由 StartBuiltinRooms 创建，不在 New 里做——
// 这样调用方可以先打日志再启动，测试里也能只测房间逻辑。
func New(opts Options) *Hub {
	if opts.Registry == nil {
		opts.Registry = provider.NewRegistry()
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.VoteSkipRatio <= 0 || opts.VoteSkipRatio > 1 {
		opts.VoteSkipRatio = 0.3
	}
	if opts.IdleTimeout < 0 {
		opts.IdleTimeout = 0
	}
	h := &Hub{
		rooms:         make(map[string]*Room),
		reg:           opts.Registry,
		primary:       opts.PrimaryProvider,
		urls:          newURLCache(),
		log:           opts.Logger,
		nowFn:         opts.Now,
		rootPassword:  opts.RootPassword,
		adminPassword: opts.AdminPassword,
		voteRatio:     opts.VoteSkipRatio,
		idleTimeout:   opts.IdleTimeout,
		radioRooms:    append([]config.RadioRoom(nil), opts.RadioRooms...),
	}
	h.presets = buildPresets(h.radioRooms, h.primary)
	return h
}

// buildPresets 把配置里的内置电台房转成前端可选的电台预设。
//
// 第一个电台房的 id 固定为 "touhou"（docs §4.1 的示例值），其余用
// radio-2、radio-3……；demo 模式额外给一个"演示电台"预设。
func buildPresets(rooms []config.RadioRoom, primary string) []Preset {
	out := make([]Preset, 0, len(rooms)+1)
	for i, r := range rooms {
		id := fmt.Sprintf("radio-%d", i+1)
		if i == 0 {
			id = "touhou"
		}
		pl := append([]string(nil), r.Playlists...)
		out = append(out, Preset{ID: id, Label: r.Name, Playlists: pl})
	}
	if primary == "demo" {
		out = append(out, Preset{ID: "demo", Label: "东方电台（演示）", Playlists: []string{"demo-radio"}})
	}
	return out
}

// PrimaryProvider 返回实际生效的主 provider（demo 或 wy）。
func (h *Hub) PrimaryProvider() string { return h.primary }

// Presets 返回电台预设的副本（httpapi 用）。
func (h *Hub) Presets() []Preset {
	out := make([]Preset, len(h.presets))
	for i, p := range h.presets {
		out[i] = Preset{ID: p.ID, Label: p.Label, Playlists: append([]string(nil), p.Playlists...)}
	}
	return out
}

// PresetByID 按 id 找预设。
func (h *Hub) PresetByID(id string) (Preset, bool) {
	for _, p := range h.presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// Now 返回服务器毫秒时间（协议里的 now/startedAt 都用它）。
func (h *Hub) Now() int64 { return h.nowFn().UnixMilli() }

// Run 启动房间主循环：每 500ms 检查一次"该切歌了吗 / 电台该补歌了吗"，
// 并定期回收空置普通房间。退出由 ctx 取消。
func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			h.Shutdown()
			return
		case <-t.C:
			h.tick()
		}
	}
}

// Shutdown 标记 hub 关闭并断开所有 WS 连接。
// （http.Server.Shutdown 不会关闭已升级的 WebSocket，得我们自己来。）
func (h *Hub) Shutdown() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	rooms := make([]*Room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	h.mu.Unlock()

	for _, r := range rooms {
		r.mu.Lock()
		clients := make([]*Client, 0, len(r.clients))
		for c := range r.clients {
			clients = append(clients, c)
		}
		r.closed = true
		r.mu.Unlock()
		for _, c := range clients {
			c.close()
		}
	}
}

// StartBuiltinRooms 按配置创建内置电台房。启动时保证存在，且永不回收。
func (h *Hub) StartBuiltinRooms() {
	for i, rr := range h.radioRooms {
		id := fmt.Sprintf("radio-%d", i+1)
		if i == 0 {
			id = "touhou"
		}
		h.mu.Lock()
		if _, exists := h.rooms[id]; exists {
			h.mu.Unlock()
			continue
		}
		r := newRoom(h, id, rr.Name, rr.Desc)
		r.isRadio = true
		r.builtin = true
		r.modeRadio = true
		r.playlistIDs = append([]string(nil), rr.Playlists...)
		h.rooms[id] = r
		count := len(h.rooms)
		h.mu.Unlock()
		h.log.Info("已创建内置电台房", "id", id, "name", rr.Name, "rooms", count)
	}
}

// tick 是一次房间巡检：推进播放、补电台队列，最后回收空房。
func (h *Hub) tick() {
	now := h.Now()
	h.mu.RLock()
	rooms := make([]*Room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	h.mu.RUnlock()

	for _, r := range rooms {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			continue
		}
		due := false
		if len(r.queue) > 0 && r.startedAt > 0 {
			durMs := int64(trackDuration(r.queue[0])) * 1000
			due = now >= r.startedAt+durMs
		}
		needRefill := (r.isRadio || r.modeRadio) && len(r.queue) < radioMinQueue &&
			!r.refilling && !r.busy && !r.refillAfter.After(h.nowFn())
		r.mu.Unlock()

		if due {
			// 放到 goroutine 里：解析 URL 可能耗到超时（8s），不能让一个房间
			// 的慢解析拖住其它房间的切歌/补歌巡检。
			go r.advance("auto")
		} else if needRefill {
			go r.refillRadio()
		}
	}
	h.reapIdle()
}

// reapIdle 回收空置的普通房间。内置房 / 电台房永不回收。
func (h *Hub) reapIdle() {
	if h.idleTimeout <= 0 {
		return
	}
	now := h.nowFn()
	h.mu.Lock()
	var dead []*Room
	var deadIDs []string
	for id, r := range h.rooms {
		if r.builtin || r.isRadio {
			continue
		}
		r.mu.Lock()
		empty := len(r.clients) == 0 && !r.emptySince.IsZero() && now.Sub(r.emptySince) >= h.idleTimeout
		if empty {
			r.closed = true
		}
		r.mu.Unlock()
		if empty {
			dead = append(dead, r)
			deadIDs = append(deadIDs, id)
		}
	}
	for _, id := range deadIDs {
		delete(h.rooms, id)
	}
	h.mu.Unlock()

	for i, r := range dead {
		h.log.Info("回收空置房间", "id", deadIDs[i], "name", r.name)
	}
}

// Rooms 返回房间列表（按内置/电台/在线数排序）。
func (h *Hub) Rooms() []model.RoomSummary {
	h.mu.RLock()
	rooms := make([]*Room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	h.mu.RUnlock()

	out := make([]model.RoomSummary, 0, len(rooms))
	for _, r := range rooms {
		if s, ok := r.summary(); ok {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Builtin != b.Builtin {
			return a.Builtin
		}
		if a.Radio != b.Radio {
			return a.Radio
		}
		if a.Online != b.Online {
			return a.Online > b.Online
		}
		return a.Name < b.Name
	})
	return out
}

// RoomCount 返回房间数（/api/healthz 用）。
func (h *Hub) RoomCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms)
}

// Room 按 id 找房间。
func (h *Hub) Room(id string) (*Room, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	r, ok := h.rooms[id]
	return r, ok
}

// ValidationError 表示创建房间时的参数错误，httpapi 据此返回 400。
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

var errHubClosed = errors.New("服务正在关闭")

// CreateRoom 创建普通房间（radio=false）。
func (h *Hub) CreateRoom(name, desc string, needPwd bool, password string) (*Room, error) {
	name = strings.TrimSpace(name)
	desc = strings.TrimSpace(desc)
	if n := utf8.RuneCountInString(name); n < 1 || n > 33 {
		return nil, &ValidationError{Message: "房间名需为 1–33 字"}
	}
	if utf8.RuneCountInString(desc) > 133 {
		return nil, &ValidationError{Message: "房间简介不能超过 133 字"}
	}
	password = strings.TrimSpace(password)
	if needPwd && (utf8.RuneCountInString(password) < 1 || utf8.RuneCountInString(password) > 32) {
		return nil, &ValidationError{Message: "房间密码需为 1–32 字"}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errHubClosed
	}
	id := ""
	for i := 0; i < 20; i++ {
		id = "r_" + randRoomID()
		if _, exists := h.rooms[id]; !exists {
			break
		}
		id = ""
	}
	if id == "" {
		return nil, errors.New("房间 ID 生成失败")
	}
	r := newRoom(h, id, name, desc)
	r.needPwd = needPwd
	r.password = password
	// 从创建那一刻起就计入空置：开完房没人进，也要能被回收。
	r.emptySince = h.nowFn()
	h.rooms[id] = r
	h.log.Info("创建房间", "id", id, "name", name)
	return r, nil
}

const roomIDChars = "abcdefghijklmnopqrstuvwxyz0123456789"

func randRoomID() string {
	b := make([]byte, 6)
	for i := range b {
		b[i] = roomIDChars[rand.IntN(len(roomIDChars))]
	}
	return string(b)
}

// trackDuration 返回曲目时长（秒），异常值兜底 240 秒，避免 0 秒导致无限切歌。
func trackDuration(t model.Track) int {
	if t.Duration > 0 {
		return t.Duration
	}
	// 时长未知时不要把歌提前切断：客户端点歌会带上 duration，老客户端/异常数据
	// 才走到这里，用 300 秒兜底（原来是 240，4 分钟以上的曲子会被拦腰截断）。
	return 300
}
