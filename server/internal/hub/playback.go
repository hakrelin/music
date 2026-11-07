package hub

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/hakrelin/music/server/internal/model"
)

type prepareOut struct {
	T     string       `json:"t"`
	Track *model.Track `json:"track"`
}

// nextIndexLocked pins random selection so preparation and advance agree.
func (r *Room) nextIndexLocked() int {
	if len(r.queue) == 0 {
		return -1
	}
	if len(r.queue) == 1 {
		return 0
	}
	if r.random {
		for i := 1; i < len(r.queue); i++ {
			if r.queue[i].QueueID == r.nextID {
				return i
			}
		}
		i := 1 + rand.IntN(len(r.queue)-1)
		r.nextID = r.queue[i].QueueID
		return i
	}
	r.nextID = r.queue[1].QueueID
	return 1
}

// Called under the room lock; all network work is performed in the background.
func (r *Room) prepareNextLocked() {
	if r.closed {
		return
	}
	var target model.Track
	if len(r.queue) > 1 {
		target = r.queue[r.nextIndexLocked()]
	}
	if r.prepareFor != target.QueueID {
		if r.prepareCancel != nil {
			r.prepareCancel()
		}
		r.prepareGeneration++
		r.prepareFor = target.QueueID
		r.preparing = false
		r.prepared = nil
		r.prepareExpiry = time.Time{}
		r.prepareRetry = time.Time{}
		r.broadcastLocked(mustJSON(prepareOut{T: "prepare"}))
	}
	if target.QueueID == "" || r.preparing || time.Now().Before(r.prepareRetry) {
		return
	}
	if r.prepared != nil && time.Now().Before(r.prepareExpiry) {
		return
	}
	failedURL := ""
	if r.prepared != nil {
		failedURL = r.prepared.URL
	}
	r.preparing = true
	r.prepareGeneration++
	generation := r.prepareGeneration
	ctx, cancel := context.WithTimeout(context.Background(), providerTimeout)
	r.prepareCancel = cancel
	go func() {
		defer cancel()
		err := r.hub.resolveMedia(ctx, &target, failedURL, false)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.closed || generation != r.prepareGeneration {
			return
		}
		r.preparing = false
		r.prepareCancel = nil
		if err != nil {
			r.prepareRetry = time.Now().Add(5 * time.Second)
			return
		}
		r.prepared = &target
		r.prepareExpiry = time.Now().Add(2 * time.Minute)
		r.broadcastLocked(mustJSON(prepareOut{T: "prepare", Track: &target}))
	}()
}

// Lyrics may arrive later; they must never hold up audio or overwrite another play epoch.
func (r *Room) loadLyrics(track model.Track, started int64) {
	r.hub.fillLyric(context.Background(), &track)
	if track.Lyric == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || len(r.queue) == 0 || r.queue[0].QueueID != track.QueueID || r.startedAt != started {
		return
	}
	r.queue[0].Lyric = track.Lyric
	r.queue[0].LyricTrans = track.LyricTrans
	r.broadcastLocked(mustJSON(map[string]any{"t": "lyric", "queueId": track.QueueID, "startedAt": started, "lyric": track.Lyric, "lyricTrans": track.LyricTrans}))
}

func (c *Client) handleMediaRefresh(m *inMsg) {
	if time.Since(c.lastMediaRefresh) < 2*time.Second {
		c.sendErrorCode("rate_limited", "播放地址刷新过于频繁", m.Req)
		return
	}
	c.room.mu.Lock()
	var track model.Track
	// Only the current or prepared next item may be refreshed; never fetch client-supplied URLs.
	for i, t := range c.room.queue {
		if t.QueueID == m.ID && (i == 0 || t.QueueID == c.room.nextID) {
			track = t
			break
		}
	}
	c.room.mu.Unlock()
	if track.QueueID == "" {
		c.sendErrorCode("not_found", "播放曲目已变化", m.Req)
		return
	}
	c.lastMediaRefresh = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), providerTimeout)
	defer cancel()
	if err := c.hub.resolveMedia(ctx, &track, m.FailedURL, m.Lower); err != nil {
		c.sendErrorCode("provider_error", "播放地址刷新失败", m.Req)
		return
	}
	c.room.mu.Lock()
	valid := false
	for i, t := range c.room.queue {
		if t.QueueID == m.ID && (i == 0 || t.QueueID == c.room.nextID) {
			valid = true
			break
		}
	}
	c.room.mu.Unlock()
	if !valid {
		c.sendErrorCode("not_found", "播放曲目已变化", m.Req)
		return
	}
	c.sendJSON(map[string]any{"t": "media.refresh", "req": m.Req, "track": track})
}
