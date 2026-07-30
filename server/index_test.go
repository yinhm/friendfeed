package server

import (
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eapache/queue"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/store"
)

func TestFeedIndex(t *testing.T) {
	// Given feed index, push and rebuild
	entryID := "c6f8dca854f011ddb489003048343a40"
	index := NewFeedIndex(nil, "public", uuid.Must(uuid.NewV4()))

	for range 10 {
		// index.itemCh <- uuid
		index.Push(entryID)
	}

	index.rebuild(nil)
	assert.Equal(t, len(index.bufq), MinQueue)
	assert.Equal(t, index.bufq[0], "c6f8dca854f011ddb489003048343a40")
	for i := 1; i < len(index.bufq); i++ {
		assert.Equal(t, index.bufq[i], "")
	}

	for i := range MinQueue {
		entryID := fmt.Sprintf("uuid-%d", i)
		index.Push(entryID)
	}

	index.rebuild(nil)
	for i := 0; i < len(index.bufq); i++ {
		assert.NotEqual(t, index.bufq[i], "")
		assert.NotEqual(t, index.bufq[i], "c6f8dca854f011ddb489003048343a40")
	}

	index.remove(5)
	assert.Equal(t, MinQueue-1, len(index.bufq))
	assert.Equal(t, 1000, cap(index.bufq))

	index.Stop()
}

func TestFeedIndexRebuildFullDuplicateBuffer(t *testing.T) {
	oldbuf := make([]string, MinQueue)
	for i := range oldbuf {
		oldbuf[i] = "duplicate"
	}

	pending := queue.New()
	pending.Add("duplicate")
	index := &FeedIndex{
		bufq:  oldbuf,
		iq:    pending,
		dirty: true,
	}

	index.rebuild(nil)

	assert.Len(t, index.bufq, MinQueue)
	assert.Equal(t, "duplicate", index.bufq[0])
	for i := 1; i < len(index.bufq); i++ {
		assert.Empty(t, index.bufq[i])
	}
}

func TestFeedIndexSnapshotIsIndependent(t *testing.T) {
	index := &FeedIndex{bufq: []string{"first", "second"}}

	snapshot := index.snapshot()
	snapshot[0] = "changed"

	assert.Equal(t, "first", index.bufq[0])
}

func TestFeedIndexPushDoesNotBlockWhenNotificationPending(t *testing.T) {
	index := &FeedIndex{
		iq:     queue.New(),
		itemCh: make(chan string, 1),
	}
	index.itemCh <- "pending"

	done := make(chan struct{})
	go func() {
		index.Push("next")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Push blocked on a full notification channel")
	}

	assert.True(t, index.dirty)
	assert.Equal(t, 1, index.iq.Length())
	assert.Equal(t, "next", index.iq.Get(0))
}

func TestRebuildFeedBufferPreservesOrderDeduplicatesAndDropsMissing(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	liveKey := []byte("live-entry")
	if err := db.Put(liveKey, []byte("entry")); err != nil {
		t.Fatal(err)
	}
	live := hex.EncodeToString(liveKey)
	missing := hex.EncodeToString([]byte("missing-entry"))

	got := rebuildFeedBuffer(
		db,
		[]string{live, missing, live},
		[]string{"older", live},
	)

	assert.Equal(t, live, got[0])
	for i := 1; i < len(got); i++ {
		assert.Empty(t, got[i])
	}
}

func TestFeedIndexConcurrentPushAndRebuildDoesNotLosePendingItems(t *testing.T) {
	index := &FeedIndex{
		bufq:   make([]string, MinQueue),
		iq:     queue.New(),
		itemCh: make(chan string, 1),
	}

	const items = 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range items {
			index.Push(fmt.Sprintf("item-%d", i))
		}
	}()
	go func() {
		defer wg.Done()
		for range 20 {
			index.rebuild(nil)
		}
	}()
	wg.Wait()
	index.rebuild(nil)

	seen := make(map[string]bool)
	for _, item := range index.snapshot() {
		if item != "" {
			seen[item] = true
		}
	}
	for i := range items {
		item := fmt.Sprintf("item-%d", i)
		if !seen[item] {
			t.Errorf("concurrent Push lost %q", item)
		}
	}
}
