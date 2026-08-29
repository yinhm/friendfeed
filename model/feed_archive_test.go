package model

import (
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/protobuf/proto"
)

func addFeedArchiveEntry(t *testing.T, db *store.Store, feed uuid.UUID, at time.Time) (uuid.UUID, store.Key) {
	t.Helper()
	entry := uuid.Must(uuid.NewV4())
	raw, err := proto.Marshal(&pb.Entry{Id: entry.String(), ProfileUuid: feed.String(), FeedUuid: feed.String(), Date: at.Format(time.RFC3339)})
	require.NoError(t, err)
	require.NoError(t, db.Set(Entry.PrefixAppend(entry.Bytes()), raw))
	key, err := EntryIndexKey(feed, entry, at)
	require.NoError(t, err)
	require.NoError(t, db.Set(key, nil))
	return entry, key
}

func TestBuildFeedArchiveCountsYearsAndBuildsBoundaryCursors(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	feed := uuid.Must(uuid.NewV4())
	_, newest := addFeedArchiveEntry(t, db, feed, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	addFeedArchiveEntry(t, db, feed, time.Date(2025, 12, 3, 0, 0, 0, 0, time.UTC))
	addFeedArchiveEntry(t, db, feed, time.Date(2025, 6, 3, 0, 0, 0, 0, time.UTC))
	_, last2025 := addFeedArchiveEntry(t, db, feed, time.Date(2025, 3, 4, 0, 0, 0, 0, time.UTC))
	addFeedArchiveEntry(t, db, feed, time.Date(2023, 5, 6, 0, 0, 0, 0, time.UTC))

	// An orphan derived row is not part of the rendered Feed and must not
	// affect totals or year boundaries.
	orphanKey, err := EntryIndexKey(feed, uuid.Must(uuid.NewV4()), time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.NoError(t, db.Set(orphanKey, nil))

	stats, err := BuildFeedArchive(db, feed)
	require.NoError(t, err)
	require.Equal(t, int64(5), stats.EntryCount)
	require.Equal(t, []int32{2026, 2025, 2023}, []int32{stats.Years[0].Year, stats.Years[1].Year, stats.Years[2].Year})
	require.Equal(t, []int64{1, 3, 1}, []int64{stats.Years[0].EntryCount, stats.Years[1].EntryCount, stats.Years[2].EntryCount})

	prefix := NewUUIDKey(TableEntryIndex, feed)
	decodeBoundary := func(cursor string) store.Key {
		position, err := util.Base58Decode(cursor)
		require.NoError(t, err)
		return NewKeyFrom(prefix, position)
	}
	// The newest year has no newer boundary: no cursor, the link is the
	// Feed's first page.
	require.Empty(t, stats.Years[0].Cursor)
	// Each older year anchors on the last row of the previous (newer) year,
	// so skipping the anchor lands on the target year's newest Entry. A
	// multi-entry year must anchor on its final row, never on a mid-year row.
	require.Equal(t, newest, decodeBoundary(stats.Years[1].Cursor))
	require.Equal(t, last2025, decodeBoundary(stats.Years[2].Cursor))
}

func TestFeedArchiveRoundTripDirtyMarkerAndPublish(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	feed := uuid.Must(uuid.NewV4())
	want := &pb.FeedArchiveStats{EntryCount: 3, Years: []*pb.FeedArchiveYear{{Year: 2026, EntryCount: 3}}}
	require.NoError(t, PutFeedArchive(db, feed, want))
	got, err := GetFeedArchive(db, feed)
	require.NoError(t, err)
	require.Equal(t, FeedArchiveVersion, got.Version)
	require.Equal(t, want.EntryCount, got.EntryCount)

	dirtyAt := time.Date(2026, 8, 1, 2, 3, 4, 0, time.UTC)
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageMarkFeedArchiveDirty(db, batch, feed, dirtyAt)
	}))
	got, err = GetFeedArchive(db, feed)
	require.NoError(t, err)
	require.Equal(t, want.EntryCount, got.EntryCount)
	gotDirtyAt, err := FeedArchiveDirtySince(db, feed)
	require.NoError(t, err)
	require.Equal(t, dirtyAt, gotDirtyAt)

	// Further mutations retain the first dirty time.
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageMarkFeedArchiveDirty(db, batch, feed, dirtyAt.Add(48*time.Hour))
	}))
	gotDirtyAt, err = FeedArchiveDirtySince(db, feed)
	require.NoError(t, err)
	require.Equal(t, dirtyAt, gotDirtyAt)

	require.NoError(t, PutFeedArchive(db, feed, want))
	_, err = FeedArchiveDirtySince(db, feed)
	require.ErrorIs(t, err, store.ErrNotFound)

	// A snapshot written by an older payload version is rejected so the read
	// path stages a rebuild instead of serving stale cursors.
	raw, err := proto.Marshal(&pb.FeedArchiveStats{Version: FeedArchiveVersion - 1, EntryCount: 9})
	require.NoError(t, err)
	require.NoError(t, db.Set(FeedArchiveMetaKey(feed), raw))
	_, err = GetFeedArchive(db, feed)
	require.Error(t, err)
}
