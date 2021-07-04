package server

import (
	"fmt"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
)

func TestFeedIndex(t *testing.T) {
	// Given feed index, push and rebuild
	uuid1 := "c6f8dca854f011ddb489003048343a40"
	index := NewFeedIndex("public", new(uuid.UUID))

	for i := 0; i < 10; i++ {
		// index.itemCh <- uuid
		index.Push(uuid1)
	}

	index.rebuild()
	assert.Equal(t, len(index.bufq), MinQueue)
	assert.Equal(t, index.bufq[0], "c6f8dca854f011ddb489003048343a40")
	for i := 1; i < len(index.bufq); i++ {
		assert.Equal(t, index.bufq[i], "")
	}

	for i := 0; i < MinQueue; i++ {
		uuid1 := fmt.Sprintf("uuid-%d", i)
		index.Push(uuid1)
	}

	index.rebuild()
	for i := 0; i < len(index.bufq); i++ {
		assert.NotEqual(t, index.bufq[i], "")
		assert.NotEqual(t, index.bufq[i], "c6f8dca854f011ddb489003048343a40")
	}

	index.doneCh <- struct{}{}
}
