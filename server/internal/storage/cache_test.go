package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReplaceFlushesLegacyIndexAndStopsWarmup(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		DataDir: dir,
		Accounts: []Account{
			{AK: "a1", SK: "s1", Bucket: "shared", Base: "https://one.example"},
			{AK: "a2", SK: "s2", Bucket: "shared", Base: "https://two.example"},
		},
	}
	items := make(map[string]*Item)
	for i := 1; i <= 831; i++ {
		id := fmt.Sprint(i)
		items["wy:"+id] = &Item{Account: i % 2, Key: "audio/wy/" + id + ".mp3", Size: int64(2048 + i)}
	}
	items["bili:BV1xx411c7mD"] = &Item{Account: 0, Key: "audio/bili/BV1xx411c7mD.m4a", Size: 4096}
	payload, err := json.Marshal(indexFile{Version: 1, Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audio-cache.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}

	cache := New(opts)
	holder := NewHolder(cache)
	t.Cleanup(holder.Close)
	if url, ok := holder.Lookup("wy", "1"); !ok || url != "https://two.example/audio/wy/1.mp3" {
		t.Fatalf("legacy cache miss or wrong account: %q", url)
	}

	started := make(chan struct{})
	done := make(chan struct{})
	holder.Ensure("bili", "pending", func(ctx context.Context, _, _ string) ([]string, error) {
		close(started)
		<-ctx.Done()
		close(done)
		return nil, ctx.Err()
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("warmup did not start")
	}
	holder.Replace(func() *Cache {
		select {
		case <-done:
		default:
			t.Error("replacement opened the index before old warmup stopped")
		}
		opts.Accounts[1].Base = "https://new-two.example"
		return New(opts)
	})

	next := holder.Get()
	if next.Count() != len(items) || next.items["wy:1"].Hits != 1 {
		t.Fatalf("replacement lost index entries or a pending hit: %v", next.Stats())
	}
	for key, item := range items {
		got := next.items[key]
		if got == nil || got.Account != item.Account || got.Key != item.Key || got.Size != item.Size {
			t.Fatalf("index changed for %s: got %+v versus %+v", key, got, item)
		}
	}
	if url, ok := holder.Lookup("wy", "1"); !ok || url != "https://new-two.example/audio/wy/1.mp3" {
		t.Fatalf("cache did not pick up the new CDN: %q", url)
	}
}
