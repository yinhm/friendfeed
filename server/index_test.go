package server

import (
	"fmt"
	"testing"

	"github.com/eapache/queue"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
)

func TestFeedIndex(t *testing.T) {
	// Given feed index, push and rebuild
	uuid1 := "c6f8dca854f011ddb489003048343a40"
	index := NewFeedIndex(nil, "public", uuid.Must(uuid.NewV4()))

	for i := 0; i < 10; i++ {
		// index.itemCh <- uuid
		index.Push(uuid1)
	}

	index.rebuild(nil)
	assert.Equal(t, len(index.bufq), MinQueue)
	assert.Equal(t, index.bufq[0], "c6f8dca854f011ddb489003048343a40")
	for i := 1; i < len(index.bufq); i++ {
		assert.Equal(t, index.bufq[i], "")
	}

	for i := 0; i < MinQueue; i++ {
		uuid1 := fmt.Sprintf("uuid-%d", i)
		index.Push(uuid1)
	}

	index.rebuild(nil)
	for i := 0; i < len(index.bufq); i++ {
		assert.NotEqual(t, index.bufq[i], "")
		assert.NotEqual(t, index.bufq[i], "c6f8dca854f011ddb489003048343a40")
	}

	index.remove(5)
	assert.Equal(t, MinQueue-1, len(index.bufq))
	assert.Equal(t, 1000, cap(index.bufq))

	index.doneCh <- struct{}{}
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
