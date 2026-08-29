package main

import (
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func addArchiveRebuildEntry(t *testing.T, db *store.Store, feed uuid.UUID, at time.Time) {
	t.Helper()
	entry := uuid.Must(uuid.NewV4())
	raw, err := proto.Marshal(&pb.Entry{Id: entry.String(), ProfileUuid: feed.String(), FeedUuid: feed.String(), Date: at.Format(time.RFC3339)})
	require.NoError(t, err)
	require.NoError(t, db.Set(model.Entry.PrefixAppend(entry.Bytes()), raw))
	key, err := model.EntryIndexKey(feed, entry, at)
	require.NoError(t, err)
	require.NoError(t, db.Set(key, nil))
}

func TestRebuildFeedArchiveDryRunAndApply(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	feed := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{Uuid: feed.String(), Id: "archive-rebuild", Type: "user"}))
	addArchiveRebuildEntry(t, db, feed, time.Date(2024, 2, 3, 0, 0, 0, 0, time.UTC))

	stats, err := rebuildFeedArchives(db, "archive-rebuild", true)
	require.NoError(t, err)
	require.Equal(t, 1, stats.feeds)
	require.Equal(t, int64(1), stats.entries)
	require.Equal(t, 1, stats.changed)
	_, err = model.GetFeedArchive(db, feed)
	require.ErrorIs(t, err, store.ErrNotFound)

	stats, err = rebuildFeedArchives(db, "archive-rebuild", false)
	require.NoError(t, err)
	require.Equal(t, 1, stats.changed)
	archive, err := model.GetFeedArchive(db, feed)
	require.NoError(t, err)
	require.Equal(t, int64(1), archive.EntryCount)
	require.Equal(t, int32(2024), archive.Years[0].Year)

	stats, err = rebuildFeedArchives(db, "archive-rebuild", false)
	require.NoError(t, err)
	require.Zero(t, stats.changed)

	// A dirty marker is freshness state in its own right. Rebuilding clears it
	// even if no Entry changed and the calculated snapshot is identical.
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return model.StageMarkFeedArchiveDirty(db, batch, feed, time.Now().UTC())
	}))
	stats, err = rebuildFeedArchives(db, "archive-rebuild", false)
	require.NoError(t, err)
	require.Equal(t, 1, stats.changed)
	_, err = model.FeedArchiveDirtySince(db, feed)
	require.ErrorIs(t, err, store.ErrNotFound)
}
