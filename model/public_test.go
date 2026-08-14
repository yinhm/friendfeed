package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestIsPublicTimeline(t *testing.T) {
	require.True(t, IsPublicTimeline(PublicTimelineUUID))
	require.False(t, IsPublicTimeline(uuid.Must(uuid.NewV4())))
	require.False(t, IsPublicTimeline(uuid.Nil))
}

func TestBumpPublicTimelineInsertAndMove(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	entry := uuid.Must(uuid.NewV4())
	base := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	require.NoError(t, BumpPublicTimeline(db, entry, base))
	position, err := TimelinePositionTime(db, PublicTimelineUUID, entry)
	require.NoError(t, err)
	require.Equal(t, base, position)

	// A later event moves the row; an older one must not.
	require.NoError(t, BumpPublicTimeline(db, entry, base.Add(time.Hour)))
	position, err = TimelinePositionTime(db, PublicTimelineUUID, entry)
	require.NoError(t, err)
	require.Equal(t, base.Add(time.Hour), position)
	require.NoError(t, BumpPublicTimeline(db, entry, base))
	position, err = TimelinePositionTime(db, PublicTimelineUUID, entry)
	require.NoError(t, err)
	require.Equal(t, base.Add(time.Hour), position)

	// Exactly one index row survives the moves.
	rows := 0
	_, err = db.ForwardScan(TimelineIndexPrefix(PublicTimelineUUID), func(_ int, _, _ []byte) error {
		rows++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, rows)

	// The public viewer never touches Home-only state.
	_, err = TimelineLastAccess(db, PublicTimelineUUID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestTrimPublicTimelineKeepsNewest(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	base := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	over := PublicTimelineMaxEntries + 10
	for i := 0; i < over; i++ {
		require.NoError(t, BumpPublicTimeline(db, uuid.Must(uuid.NewV4()), base.Add(time.Duration(i)*time.Second)))
	}
	deleted, err := TrimPublicTimeline(db, base.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, 10, deleted)

	rows := 0
	oldest := time.Time{}
	_, err = db.ForwardScan(TimelineIndexPrefix(PublicTimelineUUID), func(_ int, key, _ []byte) error {
		_, _, activity, err := ParseTimelineIndexKey(key)
		if err != nil {
			return err
		}
		oldest = activity // forward scan ends at the oldest row
		rows++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, PublicTimelineMaxEntries, rows)
	require.Equal(t, base.Add(10*time.Second), oldest)

	// Positions were deleted in pairs.
	positions := 0
	_, err = db.ForwardScan(NewKeyFrom(TimelinePosition.Prefix, PublicTimelineUUID.Bytes()), func(_ int, _, _ []byte) error {
		positions++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, PublicTimelineMaxEntries, positions)
}

func TestBuildPublicTimelineSelectsNewestByPublishTime(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	feedA := uuid.Must(uuid.NewV4())
	feedB := uuid.Must(uuid.NewV4())
	base := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	put := func(feed uuid.UUID, day int) uuid.UUID {
		entryUUID := uuid.Must(uuid.NewV4())
		_, err := PutEntry(db, &pb.Entry{
			Id:          entryUUID.String(),
			Date:        base.AddDate(0, 0, day).Format(time.RFC3339),
			ProfileUuid: feed.String(),
		})
		require.NoError(t, err)
		return entryUUID
	}
	oldA := put(feedA, 0)
	newA := put(feedA, 2)
	newB := put(feedB, 3)

	rows, err := BuildPublicTimeline(db, []uuid.UUID{feedA, feedB}, 2, TimelineRetentionMax, base.AddDate(0, 0, 4))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.NotContains(t, rows, oldA)
	require.Contains(t, rows, newA)
	require.Contains(t, rows, newB)

	// Activity equals publish time, never the rebuild time.
	require.Equal(t, base.AddDate(0, 0, 3), rows[newB])
}

func TestReplacePublicTimelineSwapsRows(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	base := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	stale := uuid.Must(uuid.NewV4())
	require.NoError(t, BumpPublicTimeline(db, stale, base))

	fresh := uuid.Must(uuid.NewV4())
	rows := map[uuid.UUID]time.Time{fresh: base.Add(time.Hour)}
	require.NoError(t, ReplacePublicTimeline(db, rows))

	_, err = TimelinePositionTime(db, PublicTimelineUUID, stale)
	require.ErrorIs(t, err, store.ErrNotFound)
	position, err := TimelinePositionTime(db, PublicTimelineUUID, fresh)
	require.NoError(t, err)
	require.Equal(t, base.Add(time.Hour), position)
}
