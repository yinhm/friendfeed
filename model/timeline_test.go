package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/store"
)

func TestMoveTimelineEntryIsMonotonicAndOrdered(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	viewer := uuid.Must(uuid.NewV4())
	first := uuid.Must(uuid.NewV4())
	second := uuid.Must(uuid.NewV4())
	base := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	_, err = MoveTimelineEntry(db, viewer, first, base, nil)
	require.NoError(t, err)
	moved, err := MoveTimelineEntry(db, viewer, first, base.Add(-time.Minute), nil)
	require.NoError(t, err)
	require.False(t, moved)
	_, err = MoveTimelineEntry(db, viewer, first, base.Add(time.Hour), nil)
	require.NoError(t, err)
	_, err = MoveTimelineEntry(db, viewer, second, base.Add(2*time.Hour), nil)
	require.NoError(t, err)

	var entries []uuid.UUID
	_, err = db.ForwardScan(TimelineIndexPrefix(viewer), func(_ int, key, _ []byte) error {
		_, entry, _, err := ParseTimelineIndexKey(key)
		entries = append(entries, entry)
		return err
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{second, first}, entries)
	position, err := TimelinePositionTime(db, viewer, first)
	require.NoError(t, err)
	require.Equal(t, base.Add(time.Hour), position)
}

func TestMoveTimelineEntryAppliesPerEntryCooldown(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	viewer := uuid.Must(uuid.NewV4())
	entry := uuid.Must(uuid.NewV4())
	base := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	_, err = MoveTimelineEntry(db, viewer, entry, base, nil)
	require.NoError(t, err)

	likeAt := base.Add(5 * time.Minute)
	moved, err := MoveTimelineEntry(db, viewer, entry, likeAt, func(old time.Time, exists bool) bool {
		return !exists || likeAt.Sub(old) >= LikeBumpCooldown
	})
	require.NoError(t, err)
	require.False(t, moved)
	likeAt = base.Add(11 * time.Minute)
	moved, err = MoveTimelineEntry(db, viewer, entry, likeAt, func(old time.Time, exists bool) bool {
		return !exists || likeAt.Sub(old) >= LikeBumpCooldown
	})
	require.NoError(t, err)
	require.True(t, moved)
}
