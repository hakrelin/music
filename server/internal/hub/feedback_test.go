package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hakrelin/music/server/internal/model"
	"github.com/hakrelin/music/server/internal/provider"
	"github.com/hakrelin/music/server/internal/provider/demo"
)

func testRoom(t *testing.T) (*Hub, *Room, []*Client) {
	t.Helper()
	h := New(Options{RootPassword: "server-root", AdminPassword: "server-admin", VoteSkipRatio: 0.5})
	r, err := h.CreateRoom("test", "", false, "", "local-root")
	if err != nil {
		t.Fatal(err)
	}
	clients := make([]*Client, 4)
	for i := range clients {
		c := &Client{hub: h, room: r, session: fmt.Sprint(i), send: make(chan []byte, 256), done: make(chan struct{})}
		if err := r.join(c, "", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
		clients[i] = c
	}
	r.queue = []model.Track{{ID: "song", QueueID: "playing"}, {ID: "song", QueueID: "first"}, {ID: "song", QueueID: "second"}}
	r.startedAt = h.Now()
	return h, r, clients
}

func TestRemoveVotesTargetEntryAndDeduplicate(t *testing.T) {
	_, r, c := testRoom(t)
	if code, _ := r.voteRemove(c[0], "playing"); code != "bad_request" {
		t.Fatal(code)
	}
	if code, _ := r.voteRemove(c[0], "first"); code != "" {
		t.Fatal(code)
	}
	if code, _ := r.voteRemove(c[0], "first"); code != "already_voted" {
		t.Fatal(code)
	}
	if len(r.queue) != 3 {
		t.Fatal("one vote must not pass")
	}
	r.voteRemove(c[1], "second")
	r.voteRemove(c[1], "first")
	if len(r.queue) != 2 || r.queue[0].QueueID != "playing" || r.queue[1].QueueID != "second" {
		t.Fatalf("wrong entry removed: %+v", r.queue)
	}
	if len(r.removeVotes["second"]) != 1 {
		t.Fatal("votes mixed between entries")
	}
	r.leave(c[1])
	if len(r.removeVotes["second"]) != 0 {
		t.Fatal("departed session retained its vote")
	}
	if code, _ := r.voteRemove(c[2], "first"); code != "not_found" {
		t.Fatal(code)
	}
}

func TestRemoveVotesHonorBanAndClear(t *testing.T) {
	_, r, c := testRoom(t)
	r.banSwitch = true
	if code, _ := r.voteRemove(c[0], "first"); code != "forbidden" {
		t.Fatal(code)
	}
	r.banSwitch = false
	r.voteRemove(c[0], "first")
	c[1].role = "root"
	r.clearQueue(c[1])
	if len(r.removeVotes) != 0 {
		t.Fatal("clear must discard votes")
	}
}

func TestRoomPasswordsAreScopedAndServerPasswordsStillWork(t *testing.T) {
	h, r, c := testRoom(t)
	c[0].handleAuth(&inMsg{Role: "root", Password: "local-root"})
	if c[0].role != "root" {
		t.Fatal("local owner could not authenticate")
	}
	other, _ := h.CreateRoom("other", "", false, "", "other-root")
	stranger := &Client{hub: h, room: other, role: "user", send: make(chan []byte, 256)}
	stranger.handleAuth(&inMsg{Role: "root", Password: "local-root"})
	if stranger.role != "user" {
		t.Fatal("room password escaped its room")
	}
	stranger.handleAuth(&inMsg{Role: "root", Password: "server-root"})
	if stranger.role != "root" {
		t.Fatal("server root lost access")
	}
	if code, _ := r.setOwnerPassword(c[1], "hijack"); code != "forbidden" {
		t.Fatal(code)
	}
	if code, _ := r.setOwnerPassword(c[0], "changed"); code != "" {
		t.Fatal(code)
	}
	c[2].handleAuth(&inMsg{Role: "root", Password: "local-root"})
	if c[2].role != "user" {
		t.Fatal("old password retained")
	}
	c[2].handleAuth(&inMsg{Role: "root", Password: "changed"})
	if c[2].role != "root" {
		t.Fatal("changed password unusable")
	}
	r.builtin = true
	if code, _ := r.setOwnerPassword(c[0], "builtin"); code != "forbidden" {
		t.Fatal(code)
	}
}

func TestMutationRepliesAcknowledgeOnlySuccess(t *testing.T) {
	_, _, c := testRoom(t)
	c[0].reply("", "accepted", "req1")
	c[0].reply("queue_full", "队列已满", "req2")
	for i, want := range []string{"ok", "error"} {
		var m map[string]any
		if err := json.Unmarshal(<-c[0].send, &m); err != nil {
			t.Fatal(err)
		}
		if m["t"] != want || m["req"] != fmt.Sprint("req", i+1) {
			t.Fatal(m)
		}
	}
}

// A second pick must not invalidate the URL resolution of the unchanged first item.
func TestPickDuringSlowResolveStartsAndKeepsQueue(t *testing.T) {
	reg := provider.NewRegistry()
	slow := &slowProvider{Provider: demo.New(), entered: make(chan struct{}), release: make(chan struct{})}
	reg.Register(slow)
	h := New(Options{Registry: reg, PrimaryProvider: "demo"})
	r, _ := h.CreateRoom("slow", "", false, "")
	c := &Client{hub: h, room: r, session: "s", send: make(chan []byte, 256)}
	r.join(c, "", "listener")
	r.pick(c, model.Track{ID: "d01", Source: "demo", Name: "first", Duration: 30})
	select {
	case <-slow.entered:
	case <-time.After(time.Second):
		t.Fatal("resolve not started")
	}
	r.pick(c, model.Track{ID: "d02", Source: "demo", Name: "second", Duration: 30})
	close(slow.release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		started := r.startedAt > 0
		size := len(r.queue)
		r.mu.Unlock()
		if started {
			if size != 2 {
				t.Fatal("lost queued item")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("first item never started after another pick")
}

type slowProvider struct {
	*demo.Provider
	entered chan struct{}
	release chan struct{}
}

func (p *slowProvider) Resolve(ctx context.Context, track *model.Track) error {
	close(p.entered)
	select {
	case <-p.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	track.URL = "/test.wav"
	return nil
}
