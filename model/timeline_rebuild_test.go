package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestRebuildHomeTimelineActivityReplaysCurrentInteractions(t *testing.T) {
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	entry := &pb.Entry{
		Date: base.Format(time.RFC3339),
		Likes: []*pb.Like{
			{Date: base.Add(5 * time.Minute).Format(time.RFC3339), From: &pb.Feed{Uuid: "a"}},
			{Date: base.Add(15 * time.Minute).Format(time.RFC3339), From: &pb.Feed{Uuid: "b"}},
		},
		Comments: []*pb.Comment{
			{Id: "c", Date: base.Add(16 * time.Minute).Format(time.RFC3339)},
		},
	}

	activity, skipped, err := rebuildHomeTimelineActivity(entry, base.Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Equal(t, base.Add(16*time.Minute), activity)
}

func TestRebuildHomeTimelineActivitySkipsUndatedLegacyInteractions(t *testing.T) {
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	entry := &pb.Entry{
		Date:     base.Format(time.RFC3339),
		Likes:    []*pb.Like{{Date: ""}, {Date: "invalid"}, nil},
		Comments: []*pb.Comment{{Date: base.Add(time.Hour).Format(time.RFC3339)}, {Date: ""}, nil},
	}

	activity, skipped, err := rebuildHomeTimelineActivity(entry, base.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 5, skipped)
	require.Equal(t, base.Add(time.Hour), activity)

	entry.Date = ""
	_, _, err = rebuildHomeTimelineActivity(entry, base.Add(2*time.Hour))
	require.ErrorContains(t, err, "invalid publish date")
}

func TestLoadHomeTimelineActivitiesUsesIndependentInteractions(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	author := uuid.Must(uuid.NewV4())
	actor := uuid.Must(uuid.NewV4())
	entryID := uuid.Must(uuid.NewV4())
	commentID := uuid.Must(uuid.NewV4())
	_, err = PutEntry(db, &pb.Entry{
		Id: entryID.String(), ProfileUuid: author.String(), Date: base.Format(time.RFC3339),
		Likes: []*pb.Like{{
			Date: base.Add(15 * time.Minute).Format(time.RFC3339),
			From: &pb.Feed{Uuid: actor.String()},
		}},
		Comments: []*pb.Comment{{
			Id: commentID.String(), Date: base.Add(16 * time.Minute).Format(time.RFC3339),
			From: &pb.Feed{Uuid: actor.String()},
		}},
	})
	require.NoError(t, err)

	missing := uuid.Must(uuid.NewV4())
	rows := map[uuid.UUID]time.Time{entryID: {}, missing: {}}
	skipped, err := loadHomeTimelineActivities(db, rows, base.Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Equal(t, base.Add(16*time.Minute), rows[entryID])
	_, exists := rows[missing]
	require.False(t, exists)
}

func TestReplaceHomeTimelineClearsStaleAndWritesRows(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	viewer := uuid.Must(uuid.NewV4())
	stale := uuid.Must(uuid.NewV4())
	_, err = MoveTimelineEntry(db, viewer, stale, time.Unix(1, 0).UTC(), nil)
	require.NoError(t, err)

	base := time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC)
	rows := map[uuid.UUID]time.Time{
		uuid.Must(uuid.NewV4()): base,
		uuid.Must(uuid.NewV4()): base.Add(time.Millisecond),
		uuid.Must(uuid.NewV4()): base.Add(2 * time.Millisecond),
	}
	require.NoError(t, ReplaceHomeTimeline(db, viewer, rows))

	count, err := db.ForwardScan(TimelineIndexPrefix(viewer), func(_ int, key, _ []byte) error {
		_, entry, activity, err := ParseTimelineIndexKey(key)
		if err != nil {
			return err
		}
		require.Equal(t, rows[entry], activity)
		position, err := TimelinePositionTime(db, viewer, entry)
		require.NoError(t, err)
		require.Equal(t, activity, position)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, len(rows), count)
	_, err = TimelinePositionTime(db, viewer, stale)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestMergeAndRemoveHomeFeedAreRelationshipScoped(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	viewer := uuid.Must(uuid.NewV4())
	feed := uuid.Must(uuid.NewV4())
	otherFeed := uuid.Must(uuid.NewV4())
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 105; i++ {
		entry := uuid.Must(uuid.NewV4())
		_, err := PutEntry(db, &pb.Entry{
			Id: entry.String(), ProfileUuid: feed.String(), FeedUuid: feed.String(),
			Date: now.Add(-time.Duration(i) * time.Second).Format(time.RFC3339),
		})
		require.NoError(t, err)
	}
	unrelated := uuid.Must(uuid.NewV4())
	_, err = Entry.Put(db, unrelated.Bytes(), &pb.Entry{
		Id: unrelated.String(), ProfileUuid: otherFeed.String(), FeedUuid: otherFeed.String(),
		Date: now.Format(time.RFC3339),
	})
	require.NoError(t, err)
	_, err = MoveTimelineEntry(db, viewer, unrelated, now, nil)
	require.NoError(t, err)

	added, skipped, err := MergeHomeFeed(db, viewer, feed, 100, TimelineRetentionMax, now)
	require.NoError(t, err)
	require.Equal(t, 100, added)
	require.Zero(t, skipped)
	rows, err := db.ForwardScan(TimelineIndexPrefix(viewer), func(int, []byte, []byte) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 101, rows)

	removed, err := RemoveHomeFeed(db, viewer, feed)
	require.NoError(t, err)
	require.Equal(t, 100, removed)
	rows, err = db.ForwardScan(TimelineIndexPrefix(viewer), func(int, []byte, []byte) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 1, rows, "unrelated Home rows must be preserved")
	_, err = TimelinePositionTime(db, viewer, unrelated)
	require.NoError(t, err)
}
