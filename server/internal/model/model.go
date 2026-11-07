package model

// Track 是队列项，也是搜索结果项。URL 只在服务端解析后填充。
type Track struct {
	QueueID     string `json:"queueId,omitempty"`
	RemoveVotes int    `json:"removeVotes,omitempty"`
	RemoveNeed  int    `json:"removeNeed,omitempty"`
	ID          string `json:"id"`
	Source      string `json:"source"`
	Name        string `json:"name"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	Cover       string `json:"cover"`
	Duration    int    `json:"duration"` // 秒
	Fee         int    `json:"fee"`      // 0 免费 / 1 VIP / 4 付费 / 8 低音质可播
	URL         string `json:"url"`
	Nick        string `json:"nick,omitempty"`  // 点歌人
	Likes       int    `json:"likes,omitempty"` // 点赞数
	// Lyric 只在「当前播放」的曲目上填充（快照/current 消息），队列里的其它曲目为空。
	Lyric string `json:"lyric,omitempty"`
	// LyricTrans 是歌词翻译（与 Lyric 同一套时间轴，可能为空）。
	LyricTrans string `json:"lyricTrans,omitempty"`
}

// Playlist 是歌单的公开信息。
type Playlist struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Cover   string `json:"cover"`
	Creator string `json:"creator"`
	Count   int    `json:"count"`
}

// ChatMsg 是聊天/通知/系统消息。
type ChatMsg struct {
	Type    string   `json:"type"` // chat | notice | system
	Nick    string   `json:"nick"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
	TS      int64    `json:"ts"`
	Session string   `json:"session,omitempty"`
}

// NowPlaying 是房间列表里展示的当前曲目摘要。
type NowPlaying struct {
	Name   string `json:"name"`
	Artist string `json:"artist"`
	Cover  string `json:"cover"`
}

// RoomSummary 是房间列表里的公开信息。
type RoomSummary struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Desc    string      `json:"desc"`
	Radio   bool        `json:"radio"`
	NeedPwd bool        `json:"needPwd"`
	Online  int         `json:"online"`
	Builtin bool        `json:"builtin"`
	Now     *NowPlaying `json:"now,omitempty"`
}

type User struct {
	ID     string `json:"id"`
	Nick   string `json:"nick"`
	Avatar string `json:"avatar"`
}
