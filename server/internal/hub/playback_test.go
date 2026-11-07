package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider"
	"github.com/hakrelin/music/server/internal/provider/demo"
)

type playbackProvider struct {
	*demo.Provider
	mu          sync.Mutex
	calls       map[string]int
	blockID     string
	resolveGate chan struct{}
	lyricGate   chan struct{}
	lowCalls    int
}

func (p *playbackProvider) Resolve(ctx context.Context, t *model.Track) error {
	p.mu.Lock()
	p.calls[t.ID]++
	n := p.calls[t.ID]
	block := p.blockID == t.ID
	p.mu.Unlock()
	if block {
		select {
		case <-p.resolveGate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	t.URL = fmt.Sprintf("https://audio.invalid/%s-%d.mp3", t.ID, n)
	return nil
}
func (p *playbackProvider) ResolveFresh(ctx context.Context, t *model.Track, lower bool) error {
	p.mu.Lock()
	if lower {
		p.lowCalls++
	}
	p.mu.Unlock()
	return p.Resolve(ctx, t)
}
func (p *playbackProvider) Lyric(ctx context.Context, id string) (string, error) {
	if p.lyricGate != nil {
		select {
		case <-p.lyricGate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "[00:00]" + id, nil
}
func playbackRoom(t *testing.T) (*Room, *playbackProvider) {
	t.Helper()
	p := &playbackProvider{Provider: demo.New(), calls: map[string]int{}, resolveGate: make(chan struct{})}
	reg := provider.NewRegistry()
	reg.Register(p)
	h := New(Options{Registry: reg, PrimaryProvider: "demo"})
	r, _ := h.CreateRoom("playback", "", false, "")
	r.queue = []model.Track{{ID: "a", Source: "demo", QueueID: "a", Duration: 30}, {ID: "b", Source: "demo", QueueID: "b", Duration: 30}, {ID: "c", Source: "demo", QueueID: "c", Duration: 30}}
	t.Cleanup(func() {
		r.mu.Lock()
		r.closed = true
		if r.prepareCancel != nil {
			r.prepareCancel()
		}
		r.mu.Unlock()
	})
	return r, p
}
func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestLyricsDoNotBlockStartAndOldLyricsCannotOverwriteNewTrack(t *testing.T) {
	r, p := playbackRoom(t)
	gate := make(chan struct{})
	p.lyricGate = gate
	defer close(gate)
	done := make(chan struct{})
	go func() { r.start(); close(done) }()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("start waited for lyrics")
	}
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.prepared != nil })
	r.advance("manual")
	r.mu.Lock()
	if r.queue[0].ID != "b" || r.startedAt == 0 {
		t.Fatal("advance waited for lyrics")
	}
	started := r.startedAt
	r.mu.Unlock()
	// Simulate a late reply from the first track after the room already advanced.
	stale := model.Track{ID: "a", Source: "demo", QueueID: "a", Lyric: "[00:00]stale"}
	r.loadLyrics(stale, started-1)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.queue[0].Lyric != "" {
		t.Fatal("late lyrics overwrote the new track")
	}
}

func TestPreparedCandidateInvalidatesOnTopAndRandomSelectionStaysPinned(t *testing.T) {
	r, p := playbackRoom(t)
	p.blockID = "b"
	r.mu.Lock()
	r.startedAt = r.hub.Now()
	r.prepareNextLocked()
	r.mu.Unlock()
	eventually(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.calls["b"] == 1 })
	c := &Client{role: "root"}
	if code, _ := r.topTrack(c, "c"); code != "" {
		t.Fatal(code)
	}
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.prepared != nil && r.prepared.QueueID == "c" })
	close(p.resolveGate)
	eventually(t, func() bool { _, ok := r.hub.urls.get("demo:b"); return ok })
	r.mu.Lock()
	if r.prepared == nil || r.prepared.QueueID != "c" {
		t.Fatal("cancelled result replaced the new candidate")
	}
	r.random = true
	r.nextID = ""
	r.prepareNextLocked()
	id := r.nextID
	for i := 0; i < 20; i++ {
		if r.queue[r.nextIndexLocked()].QueueID != id {
			t.Fatal("random candidate changed after preload")
		}
	}
	r.mu.Unlock()
}

func TestPreloadedSwitchUsesResolvedURLWithoutWaitingForProvider(t *testing.T) {
	r, p := playbackRoom(t)
	r.mu.Lock()
	r.startedAt = r.hub.Now()
	r.prepareNextLocked()
	r.mu.Unlock()
	eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.prepared != nil })
	p.mu.Lock()
	p.blockID = "b"
	p.mu.Unlock()
	start := time.Now()
	r.advance("manual")
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("prepared switch waited for network")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.queue[0].ID != "b" || r.queue[0].URL == "" {
		t.Fatal("wrong prepared candidate")
	}
}

func TestConcurrentResolveCallsShareOneProviderRequest(t *testing.T) {
	r, p := playbackRoom(t)
	p.blockID = "shared"
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr := model.Track{ID: "shared", Source: "demo"}
			if err := r.hub.resolveTrack(context.Background(), &tr); err != nil {
				t.Error(err)
			}
		}()
	}
	eventually(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.calls["shared"] > 0 })
	close(p.resolveGate)
	wg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls["shared"] != 1 {
		t.Fatalf("%d duplicate provider requests", p.calls["shared"])
	}
}

func TestRefreshIsScopedRateLimitedAndDoesNotResetRoomClock(t *testing.T) {
	r, p := playbackRoom(t)
	track := r.queue[0]
	r.hub.resolveTrack(context.Background(), &track)
	r.queue[0] = track
	r.startedAt = r.hub.Now()
	started := r.startedAt
	c := &Client{hub: r.hub, room: r, session: "refresh", send: make(chan []byte, 32)}
	r.clients[c] = struct{}{}
	c.handleMediaRefresh(&inMsg{ID: "a", FailedURL: track.URL, Req: "one"})
	var msg struct {
		T     string
		Code  string
		Track model.Track
	}
	json.Unmarshal(<-c.send, &msg)
	if msg.T != "media.refresh" || msg.Track.URL == track.URL {
		t.Fatal("failed URL was not refreshed")
	}
	if r.startedAt != started || r.queue[0].URL != track.URL {
		t.Fatal("one listener's refresh changed the room")
	}
	c.handleMediaRefresh(&inMsg{ID: "a", FailedURL: track.URL, Req: "two"})
	json.Unmarshal(<-c.send, &msg)
	if msg.Code != "rate_limited" {
		t.Fatal("refresh was not rate limited")
	}
	c.lastMediaRefresh = time.Time{}
	c.handleMediaRefresh(&inMsg{ID: "c", Req: "three"})
	json.Unmarshal(<-c.send, &msg)
	if msg.Code != "not_found" {
		t.Fatal("arbitrary queued track could be resolved")
	}
	c.handleMediaRefresh(&inMsg{ID: "a", Lower: true, Req: "four"})
	json.Unmarshal(<-c.send, &msg)
	if msg.T != "media.refresh" || p.lowCalls != 1 {
		t.Fatal("quality fallback was not used")
	}
}

func TestResolveRetainsMetadataFilledByProvider(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(demo.New())
	h := New(Options{Registry: reg, PrimaryProvider: "demo"})
	track := model.Track{ID: "d19", Source: "demo"}
	if err := h.resolveTrack(context.Background(), &track); err != nil {
		t.Fatal(err)
	}
	if track.URL == "" || track.Name == "" || track.Duration <= 0 || track.Cover == "" {
		t.Fatalf("provider metadata lost: %+v", track)
	}
}
