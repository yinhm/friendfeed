package main

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/store"
)

func TestCompactTimelinesTrimsActiveAndRetainsInactiveColdRows(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	active := uuid.Must(uuid.NewV4())
	inactive := uuid.Must(uuid.NewV4())
	require.NoError(t, model.TouchTimelineState(db, active, now))
	for _, viewer := range []uuid.UUID{active, inactive} {
		for i := 0; i < 3; i++ {
			_, err := model.MoveTimelineEntry(db, viewer, uuid.Must(uuid.NewV4()), now.Add(-time.Duration(i)*time.Hour), nil)
			require.NoError(t, err)
		}
	}

	dry, err := compactTimelines(db, timelineCompactOptions{dryRun: true, maxRows: 2, coldRows: 1, retention: model.TimelineRetentionMax, now: now})
	require.NoError(t, err)
	require.Equal(t, 6, dry.indexes)
	require.Equal(t, 3, dry.deletedIndexes)
	require.Equal(t, 6, dry.positions)
	require.Equal(t, 3, dry.deletedPositions)

	stats, err := compactTimelines(db, timelineCompactOptions{maxRows: 2, coldRows: 1, retention: model.TimelineRetentionMax, now: now})
	require.NoError(t, err)
	require.Equal(t, 3, stats.deletedIndexes)
	require.Equal(t, 3, stats.deletedPositions)
	activeRows, err := db.ForwardScan(model.TimelineIndexPrefix(active), func(int, []byte, []byte) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 2, activeRows)
	inactiveRows, err := db.ForwardScan(model.TimelineIndexPrefix(inactive), func(int, []byte, []byte) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 1, inactiveRows)
}

func TestCompactTimelinesKeepsPublicTimelineActive(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	inactive := uuid.Must(uuid.NewV4())
	// The public viewer has no TimelineState; a plain inactive viewer has
	// none either. Compact must still treat them differently.
	for _, viewer := range []uuid.UUID{model.PublicTimelineUUID, inactive} {
		for i := 0; i < 5; i++ {
			_, err := model.MoveTimelineEntry(db, viewer, uuid.Must(uuid.NewV4()), now.Add(-time.Duration(i)*time.Hour), nil)
			require.NoError(t, err)
		}
	}

	stats, err := compactTimelines(db, timelineCompactOptions{maxRows: 3, coldRows: 1, retention: model.TimelineRetentionMax, now: now})
	require.NoError(t, err)
	require.Equal(t, 6, stats.deletedIndexes)

	publicRows, err := db.ForwardScan(model.TimelineIndexPrefix(model.PublicTimelineUUID), func(int, []byte, []byte) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 3, publicRows) // active limit, never the cold limit
	inactiveRows, err := db.ForwardScan(model.TimelineIndexPrefix(inactive), func(int, []byte, []byte) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 1, inactiveRows)
}
