package model

import (
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/store"
)

func TestGroupIndexKeyRoundTrip(t *testing.T) {
	group := uuid.Must(uuid.NewV4())
	at := time.Date(2026, 8, 23, 12, 34, 56, 789000000, time.UTC)
	key, err := GroupIndexKey(group, at)
	require.NoError(t, err)
	require.Len(t, key, groupIndexKeySize)
	gotGroup, gotAt, err := ParseGroupIndexKey(key)
	require.NoError(t, err)
	require.Equal(t, group, gotGroup)
	require.Equal(t, at, gotAt)
}

func TestStageMoveGroupIndexKeepsOneMonotonicPosition(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	group := uuid.Must(uuid.NewV4())
	first := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)

	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageCreateGroupIndex(batch, group, first)
	}))
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageMoveGroupIndex(db, batch, group, first.Add(-time.Hour))
	}))
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageMoveGroupIndex(db, batch, group, second)
	}))

	iter, err := db.NewIterator(GroupIndex.Prefix)
	require.NoError(t, err)
	defer iter.Close()
	require.True(t, iter.First())
	gotGroup, gotAt, err := ParseGroupIndexKey(iter.Key())
	require.NoError(t, err)
	require.Equal(t, group, gotGroup)
	require.Equal(t, second, gotAt)
	iter.Next()
	require.False(t, iter.Valid())
	require.NoError(t, iter.Error())
}
