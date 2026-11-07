package hub

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider"
)

// Room 是一个收听单元。docs §1：queue[0] 就是当前曲目，startedAt 是它
// 开始播放的服务器毫秒时间；推进由服务器负责。
type Room struct {
	hub *Hub

	mu       sync.Mutex
	id       string
	name     string
	desc     string
	builtin  bool // 内置房（配置声明），永不回收
	isRadio  bool // 电台房：队列不足自动补歌
	needPwd  bool
	password string

	clients map[*Client]struct{}

	queue     []model.Track
	startedAt int64  // 毫秒；0 表示当前没有播放
	version   uint64 // 队列结构版本，用于"解析 URL 期间队列被改动"的校验
	// lastAdvanceAt 是上次推进的服务器毫秒时间，用于切歌防抖（见 advance）。
	lastAdvanceAt int64

	loop      bool
	random    bool
	modeRadio bool // 播放模式里的 radio（与"房间是电台房"isRadio 区分）
	goodMode  bool // 点赞模式（goodmode 消息可改，仅广播给前端，服务端暂无额外行为）
	banChoose bool // 禁点歌
	banSwitch bool // 禁切歌/投票
	volume    int

	likes         map[string]int
	removeVotes   map[string]map[string]struct{}
	ownerPassword string
	votes         map[string]struct{}
	blacklist     map[string]bool // 禁言：session id
	messages      []model.ChatMsg
	playlistIDs   []string // 电台歌单源
	lastTrackID   string   // 上一首（补歌时避免立刻重复）

	refilling   bool
	busy        bool      // 正在解析/切换（同一时刻只允许一个状态机动作）
	refillAfter time.Time // 补歌失败后的退避时间

	closed     bool
	emptySince time.Time
}

func newRoom(h *Hub, id, name, desc string) *Room {
	return &Room{
		hub:         h,
		id:          id,
		name:        name,
		desc:        desc,
		clients:     make(map[*Client]struct{}),
		likes:       make(map[string]int),
		votes:       make(map[string]struct{}),
		removeVotes: make(map[string]map[string]struct{}),
		blacklist:   make(map[string]bool),
		volume:      80,
	}
}

// ID 返回房间 ID。
func (r *Room) ID() string { return r.id }

// CheckPassword 校验房间口令（httpapi 的 POST /api/rooms/{id}/auth 用）。
func (r *Room) CheckPassword(pwd string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	if !r.needPwd {
		return true
	}
	return pwd == r.password
}

// summary 返回房间列表项；房间已被回收时 ok=false。
func (r *Room) summary() (model.RoomSummary, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return model.RoomSummary{}, false
	}
	s := model.RoomSummary{
		ID:      r.id,
		Name:    r.name,
		Desc:    r.desc,
		Radio:   r.isRadio,
		NeedPwd: r.needPwd,
		Online:  len(r.clients),
		Builtin: r.builtin,
	}
	if len(r.queue) > 0 {
		q := r.queue[0]
		s.Now = &model.NowPlaying{Name: q.Name, Artist: q.Artist, Cover: q.Cover}
	}
	return s, true
}

// ---------- 会话上下线 ----------

// join 把连接加入房间；密码不对时返回错误码对应的哨兵错误。
// nick 会做房间内去重（同一房间不允许重名）。
func (r *Room) join(c *Client, pwd, nick string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errRoomGone
	}
	if r.needPwd && pwd != r.password {
		return errBadPassword
	}
	if _, ok := r.clients[c]; ok {
		return nil
	}
	c.nick = r.uniqueNickLocked(nick)
	c.role = "user"
	r.clients[c] = struct{}{}
	r.emptySince = time.Time{}
	c.joined = true
	return nil
}

// uniqueNickLocked 生成房间内唯一的昵称：重名时追加 #2、#3……（总长 ≤16 字）。
func (r *Room) uniqueNickLocked(base string) string {
	if base == "" {
		base = "游客"
	}
	used := make(map[string]bool, len(r.clients))
	for c := range r.clients {
		used[c.nick] = true
	}
	if !used[base] {
		return base
	}
	baseRunes := []rune(base)
	for i := 2; i < 1000; i++ {
		suffix := fmt.Sprintf("#%d", i)
		suffixLen := utf8.RuneCountInString(suffix)
		head := baseRunes
		if len(head)+suffixLen > 16 {
			head = head[:16-suffixLen]
		}
		cand := string(head) + suffix
		if !used[cand] {
			return cand
		}
	}
	return base
}

// leave 把连接移出房间（幂等）。
func (r *Room) leave(c *Client) {
	r.mu.Lock()
	if _, ok := r.clients[c]; !ok {
		r.mu.Unlock()
		return
	}
	delete(r.clients, c)
	delete(r.votes, c.session)
	for _, votes := range r.removeVotes {
		delete(votes, c.session)
	}
	r.broadcastQueueLocked()
	online := len(r.clients)
	if online == 0 {
		r.emptySince = r.hub.nowFn()
	}
	r.mu.Unlock()

	if online > 0 {
		r.broadcastPresence()
	}
}

// ---------- 出站广播（Locked 版本要求调用方已持锁） ----------

func (r *Room) broadcastLocked(payload []byte) {
	for c := range r.clients {
		c.enqueue(payload)
	}
}

func (r *Room) statePayloadLocked() []byte {
	return mustJSON(stateOut{
		T:         "state",
		Loop:      r.loop,
		Random:    r.random,
		Radio:     r.modeRadio,
		GoodMode:  r.goodMode,
		BanChoose: r.banChoose,
		BanSwitch: r.banSwitch,
		Volume:    r.volume,
	})
}

func (r *Room) queuePayloadLocked() []byte {
	q := r.queueViewLocked()
	return mustJSON(queueOut{T: "queue", Queue: q})
}

func (r *Room) currentPayloadLocked() []byte {
	if len(r.queue) == 0 || r.startedAt == 0 {
		return mustJSON(currentOut{T: "current", Track: nil, StartedAt: 0})
	}
	t := r.queue[0]
	return mustJSON(currentOut{T: "current", Track: &t, StartedAt: r.startedAt})
}

func (r *Room) presencePayloadLocked() []byte {
	return mustJSON(presenceOut{T: "presence", Online: len(r.clients), Members: r.membersLocked()})
}

// membersLocked 返回房间在线成员（昵称 + 角色），按昵称排序。
// 调用方必须持锁。
func (r *Room) membersLocked() []memberOut {
	out := make([]memberOut, 0, len(r.clients))
	for c := range r.clients {
		role := "guest"
		if c.role == "root" {
			role = "root"
		} else if c.role == "admin" {
			role = "admin"
		}
		out = append(out, memberOut{Nick: c.nick, Role: role})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nick < out[j].Nick })
	return out
}

// broadcastQueue / broadcastCurrent / broadcastState / broadcastPresence
// 是对外的不持锁版本，内部自己加锁。
func (r *Room) broadcastQueue() {
	r.mu.Lock()
	p := r.queuePayloadLocked()
	r.broadcastLocked(p)
	r.mu.Unlock()
}

func (r *Room) broadcastCurrent() {
	r.mu.Lock()
	p := r.currentPayloadLocked()
	r.broadcastLocked(p)
	r.mu.Unlock()
}

// broadcastQueueLocked / broadcastCurrentLocked 是持锁版本，供状态机内部使用。
func (r *Room) broadcastQueueLocked() {
	r.broadcastLocked(r.queuePayloadLocked())
}

func (r *Room) broadcastCurrentLocked() {
	r.broadcastLocked(r.currentPayloadLocked())
}

func (r *Room) broadcastState() {
	r.mu.Lock()
	p := r.statePayloadLocked()
	r.broadcastLocked(p)
	r.mu.Unlock()
}

func (r *Room) broadcastPresence() {
	r.mu.Lock()
	p := r.presencePayloadLocked()
	r.broadcastLocked(p)
	r.mu.Unlock()
}

func noticePayload(content, level string) []byte {
	return mustJSON(noticeOut{T: "notice", Content: content, Level: level})
}

// pushNotice 广播一条系统提示（不写聊天历史，与聊天区分）。
func (r *Room) pushNotice(content, level string) {
	p := noticePayload(content, level)
	r.mu.Lock()
	r.broadcastLocked(p)
	r.mu.Unlock()
}

func (r *Room) pushNoticeLocked(content, level string) {
	r.broadcastLocked(noticePayload(content, level))
}

// ---------- 供 ServeWS 使用的初始包 ----------

func (r *Room) helloPayloadLocked(c *Client) []byte {
	return mustJSON(helloOut{
		T:   "hello",
		Now: r.hub.Now(),
		Self: selfInfo{
			Session: c.session,
			Nick:    c.nick,
			Role:    c.role,
		},
		Room: roomBrief{ID: r.id, Name: r.name, Radio: r.isRadio, NeedPwd: r.needPwd},
	})
}

func (r *Room) snapshotPayloadLocked() []byte {
	q := r.queueViewLocked()
	likes := make(map[string]int, len(r.likes))
	for k, v := range r.likes {
		likes[k] = v
	}
	chat := make([]model.ChatMsg, len(r.messages))
	copy(chat, r.messages)
	return mustJSON(snapshotOut{
		T:         "snapshot",
		Online:    len(r.clients),
		Queue:     q,
		StartedAt: r.startedAt,
		Likes:     likes,
		Chat:      chat,
		Members:   r.membersLocked(),
	})
}

// ---------- 播放状态机 ----------

// start 在队列已经非空但还没开始播放时，解析第一首并启动播放。
func (r *Room) start() {
	r.mu.Lock()
	if r.closed || r.busy || r.startedAt > 0 || len(r.queue) == 0 {
		r.mu.Unlock()
		return
	}
	r.busy = true
	r.mu.Unlock()

	r.doStart()

	r.mu.Lock()
	r.busy = false
	r.mu.Unlock()
}

func (r *Room) doStart() {
	for attempt := 0; attempt < 4; attempt++ {
		r.mu.Lock()
		if r.closed || r.startedAt > 0 || len(r.queue) == 0 {
			r.mu.Unlock()
			return
		}
		cand := r.queue[0]
		r.mu.Unlock()

		err := r.hub.resolveTrack(context.Background(), &cand)
		if err == nil {
			// 只有当前播放的曲目才拉歌词（docs §2 补充契约）。
			r.hub.fillLyric(context.Background(), &cand)
		}

		r.mu.Lock()
		if r.closed || r.startedAt > 0 || len(r.queue) == 0 || r.queue[0].QueueID != cand.QueueID {
			r.mu.Unlock()
			return
		}
		if err != nil {
			// 这首放不了：从队列删掉，继续尝试下一首。
			r.queue = r.queue[1:]
			r.version++
			r.mu.Unlock()
			r.pushNotice(fmt.Sprintf("「%s」无法播放，已跳过", cand.Name), "warn")
			r.broadcastQueue()
			continue
		}
		r.queue[0] = cand
		r.startedAt = r.hub.Now()
		r.votes = make(map[string]struct{})
		r.version++
		r.broadcastCurrentLocked()
		r.broadcastQueueLocked()
		r.mu.Unlock()
		return
	}

	r.mu.Lock()
	r.queue = nil
	r.startedAt = 0
	r.version++
	r.mu.Unlock()
	r.pushNotice("队列中的曲目暂时都无法播放，已清空队列", "error")
	r.broadcastQueue()
	r.broadcastCurrent()
}

// advance 切到下一首。
//
// reason: auto（播完）/ manual（admin next）/ vote（投票通过）。
func (r *Room) advance(reason string) {
	r.mu.Lock()
	if r.closed || r.busy || len(r.queue) == 0 {
		r.mu.Unlock()
		return
	}
	// auto 触发时再核对一次是否真的到期：tick 与手动切歌可能撞车。
	if reason == "auto" && r.startedAt > 0 {
		due := r.hub.Now() >= r.startedAt+int64(trackDuration(r.queue[0]))*1000
		if !due {
			r.mu.Unlock()
			return
		}
	}
	// 防抖：1.2 秒内只允许推进一次。前端偶尔会重复发 skip.vote（按钮+快捷键、
	// 或多端同时点），没有这道闸门就会连切两首。
	if now := r.hub.Now(); now-r.lastAdvanceAt < 1200 {
		r.mu.Unlock()
		return
	} else {
		r.lastAdvanceAt = now
	}
	r.busy = true
	r.mu.Unlock()

	r.doAdvance(reason)

	r.mu.Lock()
	r.busy = false
	r.mu.Unlock()
}

func (r *Room) doAdvance(reason string) {
	for attempt := 0; attempt < 4; attempt++ {
		r.mu.Lock()
		if r.closed || len(r.queue) == 0 {
			r.mu.Unlock()
			return
		}
		version := r.version
		idx := 0
		if len(r.queue) > 1 {
			idx = 1
			if r.random {
				idx = 1 + rand.IntN(len(r.queue)-1)
			}
		} else {
			// 队列里只剩当前这一首。
			replay := r.loop || r.random || r.modeRadio || r.isRadio || reason == "manual" || reason == "vote"
			if !replay {
				// 普通房间自然播完：停止播放并清空队列。
				r.queue = nil
				r.startedAt = 0
				r.lastTrackID = ""
				r.version++
				r.mu.Unlock()
				r.broadcastQueue()
				r.broadcastCurrent()
				return
			}
			idx = 0 // 重播同一首
		}
		cand := r.queue[idx]
		r.mu.Unlock()

		err := r.hub.resolveTrack(context.Background(), &cand)
		if err == nil {
			r.hub.fillLyric(context.Background(), &cand)
		}

		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return
		}
		if r.version != version {
			// 解析期间队列被改动（点歌/删除），交给下一轮 tick 处理。
			r.mu.Unlock()
			return
		}
		if err != nil {
			r.queue = append(r.queue[:idx], r.queue[idx+1:]...)
			r.version++
			r.mu.Unlock()
			r.pushNotice(fmt.Sprintf("「%s」无法播放，已跳过", cand.Name), "warn")
			r.broadcastQueue()
			continue
		}

		newQueue := make([]model.Track, 0, len(r.queue))
		newQueue = append(newQueue, cand)
		for i, t := range r.queue {
			if i == idx || (i == 0 && idx != 0) {
				continue
			}
			newQueue = append(newQueue, t)
		}
		r.queue = newQueue
		r.startedAt = r.hub.Now()
		r.lastTrackID = cand.ID
		r.votes = make(map[string]struct{})
		r.version++
		r.broadcastCurrentLocked()
		r.broadcastQueueLocked()
		r.mu.Unlock()
		return
	}

	// 连续多次解析失败：放弃这次推进，等下一轮 tick；队列保持原样。
	r.pushNotice("曲目解析失败，稍后重试", "error")
}

// refillRadio 是服务器侧 DJ：电台房队列不足 3 首时从歌单补到 8 首。
//
// 关键点：
//   - 网络请求（拉歌单）在房间锁之外做；
//   - 补进来的曲目打乱、去重、避开上一首；
//   - 失败后退避 30 秒，避免每 500ms 打一次外部接口。
func (r *Room) refillRadio() {
	r.mu.Lock()
	if r.closed || r.refilling || r.busy {
		r.mu.Unlock()
		return
	}
	if !(r.isRadio || r.modeRadio) || len(r.queue) >= radioMinQueue {
		r.mu.Unlock()
		return
	}
	ids := append([]string(nil), r.playlistIDs...)
	last := r.lastTrackID
	r.refilling = true
	r.mu.Unlock()

	var tracks []model.Track
	if len(ids) > 0 {
		order := rand.Perm(len(ids))
		for i := 0; i < len(order) && i < 3; i++ {
			_, ts, err := r.hub.playlistTracks(context.Background(), r.hub.primary, ids[order[i]])
			if err != nil || len(ts) == 0 {
				r.hub.log.Warn("电台歌单拉取失败", "room", r.id, "playlist", ids[order[i]], "err", err)
				continue
			}
			tracks = ts
			break
		}
	}

	r.mu.Lock()
	r.refilling = false
	if r.closed {
		r.mu.Unlock()
		return
	}
	if len(tracks) == 0 {
		r.refillAfter = r.hub.nowFn().Add(30 * time.Second)
		r.mu.Unlock()
		return
	}
	target := radioTargetQueue - len(r.queue)
	if target <= 0 {
		r.mu.Unlock()
		return
	}
	rand.Shuffle(len(tracks), func(i, j int) { tracks[i], tracks[j] = tracks[j], tracks[i] })
	existing := make(map[string]bool, len(r.queue))
	for _, t := range r.queue {
		existing[t.Source+":"+t.ID] = true
	}
	picked := make([]model.Track, 0, target)
	for _, t := range tracks {
		if len(picked) >= target {
			break
		}
		if t.ID == last || existing[t.Source+":"+t.ID] {
			continue
		}
		existing[t.Source+":"+t.ID] = true
		t.URL = ""
		t.Nick = ""
		t.Likes = 0
		t.QueueID = crand.Text()
		picked = append(picked, t)
	}
	if len(picked) == 0 {
		r.mu.Unlock()
		return
	}
	r.queue = append(r.queue, picked...)
	r.version++
	r.broadcastQueueLocked()
	r.mu.Unlock()

	r.start()
}

// ---------- 队列操作 ----------

// pick 点歌：追加到队尾；若当前没有播放则立刻开始。
func (r *Room) pick(c *Client, t model.Track) (string, string) {
	t.ID = strings.TrimSpace(t.ID)
	t.Name = strings.TrimSpace(t.Name)
	if t.ID == "" {
		return "bad_request", "曲目 ID 不能为空"
	}

	if t.Source == "" {
		t.Source = r.hub.primary
	}
	// 时长兜底：搜索接口偶尔给不出 duration，点歌时按 ID 补一次，
	// 否则队列显示 00:00、自动切歌也会按 300 秒误判。
	if t.Duration <= 0 {
		if p, ok := r.hub.reg.ForTrack(t); ok {
			if dp, ok2 := p.(provider.DetailProvider); ok2 {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if meta, derr := dp.TrackDetail(ctx, t.ID); derr == nil && meta.Duration > 0 {
					t.Duration = meta.Duration
					if t.Artist == "" {
						t.Artist = meta.Artist
					}
					if t.Cover == "" {
						t.Cover = meta.Cover
					}
				}
				cancel()
			}
		}
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "room_not_found", "房间不存在"
	}
	if r.banChoose && !c.isAdmin() {
		r.mu.Unlock()
		return "forbidden", "房间已禁止点歌"
	}
	if len(r.queue) >= queueLimit {
		r.mu.Unlock()
		return "queue_full", "队列已满"
	}
	if t.Source == "" {
		t.Source = r.hub.primary
	}
	t.QueueID = crand.Text()
	t.Nick = c.nick
	t.Likes = 0
	t.URL = ""
	r.queue = append(r.queue, t)
	r.version++
	r.broadcastQueueLocked()
	needStart := r.startedAt == 0
	r.mu.Unlock()

	if needStart {
		go r.start()
	}
	return "", ""
}

// removeTrack 删除队列项：admin/root 可删任意，普通用户只能删自己点的。
func (r *Room) removeTrack(c *Client, id string) (string, string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "bad_request", "曲目 ID 不能为空"
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "room_not_found", "房间不存在"
	}
	idx := -1
	for i, t := range r.queue {
		if t.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		r.mu.Unlock()
		return "not_found", "队列里没有这首"
	}
	if !c.isAdmin() && r.queue[idx].Nick != c.nick {
		r.mu.Unlock()
		return "forbidden", "只能删除自己点的歌"
	}
	removedCurrent := idx == 0
	r.queue = append(r.queue[:idx], r.queue[idx+1:]...)
	r.version++
	if removedCurrent {
		r.startedAt = 0
		r.lastTrackID = ""
	}
	r.broadcastQueueLocked()
	if removedCurrent {
		r.broadcastCurrentLocked()
	}
	needStart := removedCurrent && len(r.queue) > 0
	r.mu.Unlock()

	if needStart {
		r.start()
	}
	return "", ""
}

// topTrack 置顶：移到"下一首"的位置（不打断当前播放）。
func (r *Room) topTrack(c *Client, id string) (string, string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "bad_request", "曲目 ID 不能为空"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "room_not_found", "房间不存在"
	}
	if !c.isAdmin() {
		return "forbidden", "没有权限"
	}
	idx := -1
	for i, t := range r.queue {
		if t.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "not_found", "队列里没有这首"
	}
	if idx == 0 {
		return "", ""
	}
	target := 0
	if r.startedAt > 0 {
		target = 1
	}
	if idx == target {
		return "", ""
	}
	t := r.queue[idx]
	r.queue = append(r.queue[:idx], r.queue[idx+1:]...)
	rest := make([]model.Track, 0, len(r.queue)+1)
	rest = append(rest, r.queue[:target]...)
	rest = append(rest, t)
	rest = append(rest, r.queue[target:]...)
	r.queue = rest
	r.version++
	r.broadcastQueueLocked()
	return "", ""
}

// clearQueue 清空队列：保留当前播放曲目，其余全部删掉。
func (r *Room) clearQueue(c *Client) (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "room_not_found", "房间不存在"
	}
	if !c.isAdmin() {
		return "forbidden", "没有权限"
	}
	if r.startedAt > 0 && len(r.queue) > 0 {
		r.queue = r.queue[:1]
	} else {
		r.queue = nil
		r.startedAt = 0
	}
	r.version++
	r.broadcastQueueLocked()
	return "", ""
}

// ---------- 投票 / 点赞 / 聊天 / 模式 ----------

// voteSkip 记一票；达到 ceil(在线人数 × voteSkipRatio) 后切歌。
func (r *Room) voteSkip(c *Client) (string, string) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "room_not_found", "房间不存在"
	}
	if r.banSwitch && !c.isAdmin() {
		r.mu.Unlock()
		return "forbidden", "房间已禁止切歌"
	}
	if r.startedAt == 0 || len(r.queue) == 0 {
		r.mu.Unlock()
		return "not_playing", "当前没有播放"
	}
	r.votes[c.session] = struct{}{}
	online := len(r.clients)
	threshold := int(math.Ceil(r.hub.voteRatio * float64(online)))
	if threshold < 1 {
		threshold = 1
	}
	got := len(r.votes)
	passed := got >= threshold
	if passed {
		r.votes = make(map[string]struct{})
	}
	r.mu.Unlock()

	if passed {
		r.advance("vote")
		return "", ""
	}
	r.pushNotice(fmt.Sprintf("投票切歌 %d/%d", got, threshold), "info")
	return "", ""
}

// good 给当前队列里的某首点赞。
func (r *Room) good(id string) (string, string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "bad_request", "曲目 ID 不能为空"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "room_not_found", "房间不存在"
	}
	found := false
	for i := range r.queue {
		if r.queue[i].ID == id {
			r.likes[id]++
			r.queue[i].Likes = r.likes[id]
			found = true
			break
		}
	}
	if !found {
		return "not_found", "队列里没有这首"
	}
	r.broadcastQueueLocked()
	return "", ""
}

// chat 发一条聊天消息。
func (r *Room) chat(c *Client, content string) (string, string) {
	if r.isBlacklisted(c.session) {
		return "banned", "你已被禁言"
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "bad_request", "消息不能为空"
	}
	if utf8.RuneCountInString(content) > 200 {
		content = string([]rune(content)[:200])
	}
	msg := model.ChatMsg{
		Type:    "chat",
		Nick:    c.nick,
		Content: content,
		TS:      r.hub.Now(),
		Session: c.session,
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "room_not_found", "房间不存在"
	}
	r.messages = append(r.messages, msg)
	if len(r.messages) > chatHistoryLimit {
		r.messages = r.messages[len(r.messages)-chatHistoryLimit:]
	}
	r.broadcastLocked(mustJSON(chatOut{T: "chat", Msg: msg}))
	r.mu.Unlock()
	return "", ""
}

// isBlacklisted 查询 session 是否被禁言。
func (r *Room) isBlacklisted(session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.blacklist[session]
}

// setBlacklist 设置/解除禁言。返回受影响客户端的连接（如果在线），用于私聊提示。
func (r *Room) setBlacklist(session string, black bool) (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "room_not_found", "房间不存在"
	}
	if black {
		r.blacklist[session] = true
	} else {
		delete(r.blacklist, session)
	}
	return "", ""
}

// notifySession 给某个 session 的在线连接发一条私密 notice。
func (r *Room) notifySession(session, content, level string) {
	p := noticePayload(content, level)
	r.mu.Lock()
	for c := range r.clients {
		if c.session == session {
			c.enqueue(p)
		}
	}
	r.mu.Unlock()
}

// setMode 设置播放模式；缺省（nil）的字段不改。
func (r *Room) setMode(c *Client, loop, random, radio *bool) (string, string) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "room_not_found", "房间不存在"
	}
	if !c.isAdmin() {
		r.mu.Unlock()
		return "forbidden", "没有权限"
	}
	if loop != nil {
		r.loop = *loop
	}
	if random != nil {
		r.random = *random
	}
	radioOn := false
	if radio != nil {
		r.modeRadio = *radio
		radioOn = *radio
	}
	p := r.statePayloadLocked()
	r.broadcastLocked(p)
	r.mu.Unlock()

	if radioOn {
		r.refillRadio()
	}
	return "", ""
}

// setVolume 设置房间音量（0–100）。服务端只存并广播，实际音量由客户端设置。
func (r *Room) setVolume(c *Client, value int) (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "room_not_found", "房间不存在"
	}
	if !c.isAdmin() {
		return "forbidden", "没有权限"
	}
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	r.volume = value
	r.broadcastLocked(r.statePayloadLocked())
	return "", ""
}

// setBan 设置 banChoose / banSwitch（协议里的 ban 消息）。
// 缺省字段不改；成功后广播 state，前端据此禁用点歌/切歌按钮。
func (r *Room) setBan(c *Client, choose, switchTo *bool) (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "room_not_found", "房间不存在"
	}
	if !c.isAdmin() {
		return "forbidden", "没有权限"
	}
	if choose != nil {
		r.banChoose = *choose
	}
	if switchTo != nil {
		r.banSwitch = *switchTo
	}
	r.broadcastLocked(r.statePayloadLocked())
	return "", ""
}

// setGoodMode 设置点赞模式（协议里的 goodmode 消息）。
func (r *Room) setGoodMode(c *Client, on bool) (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "room_not_found", "房间不存在"
	}
	if !c.isAdmin() {
		return "forbidden", "没有权限"
	}
	r.goodMode = on
	r.broadcastLocked(r.statePayloadLocked())
	return "", ""
}

// loadRadio 把预设歌单装进房间的 defaultPlaylists 并补队。
func (r *Room) loadRadio(c *Client, presetID string) (string, string) {
	if !c.isAdmin() {
		return "forbidden", "没有权限"
	}
	preset, ok := r.hub.PresetByID(presetID)
	if !ok {
		return "bad_preset", "未知的电台预设"
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "room_not_found", "房间不存在"
	}
	r.playlistIDs = append([]string(nil), preset.Playlists...)
	r.modeRadio = true
	r.refillAfter = time.Time{}
	r.version++
	r.broadcastLocked(r.statePayloadLocked())
	r.mu.Unlock()

	r.pushNotice("已切换电台：「"+preset.Label+"」", "info")
	r.refillRadio()
	return "", ""
}

// next 是 admin/root 的强制切歌（docs §4.1 的 next）。
func (r *Room) next(c *Client) (string, string) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "room_not_found", "房间不存在"
	}
	if !c.isAdmin() {
		r.mu.Unlock()
		return "forbidden", "没有权限"
	}
	empty := len(r.queue) == 0
	r.mu.Unlock()

	if empty {
		return "not_playing", "当前没有播放"
	}
	r.advance("manual")
	return "", ""
}

// queueViewLocked decorates only pending entries and discards obsolete ballots.
func (r *Room) queueViewLocked() []model.Track {
	q := append([]model.Track{}, r.queue...)
	live := map[string]bool{}
	need := max(1, int(math.Ceil(r.hub.voteRatio*float64(len(r.clients)))))
	for i := range q {
		if i == 0 {
			continue
		}
		live[q[i].QueueID] = true
		q[i].RemoveVotes = len(r.removeVotes[q[i].QueueID])
		q[i].RemoveNeed = need
	}
	for id := range r.removeVotes {
		if !live[id] {
			delete(r.removeVotes, id)
		}
	}
	return q
}

func (r *Room) voteRemove(c *Client, entry string) (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "room_not_found", "房间不存在"
	}
	if _, ok := r.clients[c]; !ok {
		return "forbidden", "请先进入房间"
	}
	if r.banSwitch && !c.isAdmin() {
		return "forbidden", "房间已禁止切歌及投票删除"
	}
	idx := -1
	for i, t := range r.queue {
		if t.QueueID == entry && entry != "" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "not_found", "队列里没有这首"
	}
	if idx == 0 {
		return "bad_request", "当前歌曲请使用投票切歌"
	}
	votes := r.removeVotes[entry]
	if votes == nil {
		votes = map[string]struct{}{}
		r.removeVotes[entry] = votes
	}
	if _, ok := votes[c.session]; ok {
		return "already_voted", "你已经投过票了"
	}
	votes[c.session] = struct{}{}
	need := max(1, int(math.Ceil(r.hub.voteRatio*float64(len(r.clients)))))
	message := fmt.Sprintf("投票删除「%s」 %d/%d", r.queue[idx].Name, len(votes), need)
	if len(votes) >= need {
		message = "投票通过，已删除「" + r.queue[idx].Name + "」"
		r.queue = append(r.queue[:idx], r.queue[idx+1:]...)
		r.version++
		delete(r.removeVotes, entry)
	}
	r.broadcastQueueLocked()
	r.pushNoticeLocked(message, "info")
	return "", message
}

func (r *Room) OwnerPassword() string { r.mu.Lock(); defer r.mu.Unlock(); return r.ownerPassword }
func (r *Room) setOwnerPassword(c *Client, password string) (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.builtin || c.role != "root" {
		return "forbidden", "只有房主可以修改普通房间的管理密码"
	}
	if n := utf8.RuneCountInString(password); n < 1 || n > 64 {
		return "bad_request", "管理密码需为 1–64 字"
	}
	r.ownerPassword = password
	return "", "房间管理密码已修改"
}
