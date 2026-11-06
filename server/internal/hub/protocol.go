package hub

import (
	"encoding/json"

	"github.com/hakrelin/music/server/internal/model"
)

// 本文件集中定义 WS 协议（docs/architecture.md §4）的线上结构。
//
// 服务端 → 客户端一律是"单个 JSON 对象 + t 字段"；客户端 → 服务端用
// 一个宽松的 inMsg 结构接收所有类型，未知字段自然被忽略，前端加字段
// 不会让老服务端报错。

// ---------- 服务器 → 客户端 ----------

type selfInfo struct {
	Session string `json:"session"`
	Nick    string `json:"nick"`
	Role    string `json:"role"`
}

type roomBrief struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Radio   bool   `json:"radio"`
	NeedPwd bool   `json:"needPwd"`
}

type helloOut struct {
	T    string    `json:"t"`
	Now  int64     `json:"now"`
	Self selfInfo  `json:"self"`
	Room roomBrief `json:"room"`
}

type stateOut struct {
	T         string `json:"t"`
	Loop      bool   `json:"loop"`
	Random    bool   `json:"random"`
	Radio     bool   `json:"radio"`
	GoodMode  bool   `json:"goodMode"`
	BanChoose bool   `json:"banChoose"`
	BanSwitch bool   `json:"banSwitch"`
	Volume    int    `json:"volume"`
}

type snapshotOut struct {
	T         string          `json:"t"`
	Online    int             `json:"online"`
	Queue     []model.Track   `json:"queue"`
	StartedAt int64           `json:"startedAt"`
	Likes     map[string]int  `json:"likes"`
	Chat      []model.ChatMsg `json:"chat"`
}

type presenceOut struct {
	T      string `json:"t"`
	Online int    `json:"online"`
}

type queueOut struct {
	T     string        `json:"t"`
	Queue []model.Track `json:"queue"`
}

type currentOut struct {
	T         string       `json:"t"`
	Track     *model.Track `json:"track"`
	StartedAt int64        `json:"startedAt"`
}

type chatOut struct {
	T   string        `json:"t"`
	Msg model.ChatMsg `json:"msg"`
}

type noticeOut struct {
	T       string `json:"t"`
	Content string `json:"content"`
	Level   string `json:"level"` // info | warn | error
}

type pongOut struct {
	T   string `json:"t"`
	Now int64  `json:"now"`
}

type searchOut struct {
	T      string        `json:"t"`
	Req    string        `json:"req,omitempty"`
	Tracks []model.Track `json:"tracks"`
	Total  int           `json:"total"`
}

type playlistSearchOut struct {
	T         string           `json:"t"`
	Req       string           `json:"req,omitempty"`
	Playlists []model.Playlist `json:"playlists"`
	Total     int              `json:"total"`
}

type playlistGetOut struct {
	T        string         `json:"t"`
	Req      string         `json:"req,omitempty"`
	Playlist model.Playlist `json:"playlist"`
	Tracks   []model.Track  `json:"tracks"`
}

type userPlaylistsOut struct {
	T         string           `json:"t"`
	Req       string           `json:"req,omitempty"`
	Playlists []model.Playlist `json:"playlists"`
}

type authOut struct {
	T       string `json:"t"`
	Role    string `json:"role"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type errorOut struct {
	T       string `json:"t"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Req     string `json:"req,omitempty"`
}

// mustJSON 序列化出站消息；理论上不会失败（结构体都是可序列化类型）。
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// 宁可发一条通用错误，也不要 panic 掉整个连接。
		return []byte(`{"t":"error","code":"internal","message":"服务器内部错误"}`)
	}
	return b
}

// ---------- 客户端 → 服务器 ----------

// inMsg 覆盖 docs §4.1 的全部字段。用指针区分"没传"和"传了零值"，
// 这对 mode（缺省字段不改）和 volume（0 是合法音量）是必须的。
type inMsg struct {
	T        string `json:"t"`
	Req      string `json:"req"`
	Nick     string `json:"nick"`
	Pwd      string `json:"pwd"`
	Content  string `json:"content"`
	ID       string `json:"id"`
	Source   string `json:"source"`
	Name     string `json:"name"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Cover    string `json:"cover"`
	Duration int    `json:"duration"`
	Fee      int    `json:"fee"`
	Q        string `json:"q"`
	Page     int    `json:"page"`
	Size     int    `json:"size"`
	UID      string `json:"uid"`
	Role     string `json:"role"`
	Password string `json:"password"`
	Preset   string `json:"preset"`
	Loop     *bool  `json:"loop"`
	Random   *bool  `json:"random"`
	Radio    *bool  `json:"radio"`
	Choose   *bool  `json:"choose"`
	Switch   *bool  `json:"switch"`
	On       *bool  `json:"on"`
	Value    *int   `json:"value"`
	Session  string `json:"session"`
}
