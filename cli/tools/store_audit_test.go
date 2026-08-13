package main

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestAuditStoreFindsIndexAndGraphDrift(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	author := uuid.Must(uuid.NewV4())
	follower := uuid.Must(uuid.NewV4())
	date := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	for range 2 {
		entryID := uuid.Must(uuid.NewV4())
		_, err := model.PutEntry(db, &pb.Entry{
			Id: entryID.String(), Date: date, ProfileUuid: author.String(),
			From: &pb.Feed{Uuid: author.String(), Id: "author"},
		})
		require.NoError(t, err)
	}

	followKey := model.NewKeyFrom(model.Follow.Prefix, follower.Bytes(), author.Bytes())
	followerKey := model.NewKeyFrom(model.Follower.Prefix, author.Bytes(), follower.Bytes())
	require.NoError(t, db.Put(followKey, []byte("1")))
	require.NoError(t, db.Put(followerKey, []byte("1")))

	orphanOwner := uuid.Must(uuid.NewV4())
	orphanEntry := uuid.Must(uuid.NewV4())
	orphanIndex, err := model.EntryIndexKey(orphanOwner, orphanEntry, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, db.Put(orphanIndex, nil))

	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 2, stats.entries)
	require.Equal(t, 3, stats.entryIndexes)
	require.Zero(t, stats.missingDirectIndexes)
	require.Equal(t, 1, stats.orphanIndexes)
	require.Equal(t, 2, stats.timelineIndexes)
	require.Equal(t, 2, stats.timelinePositions)
	require.Zero(t, stats.timelineMissingEntry)
	require.Equal(t, 1, stats.sameSecondGroups)
	require.Equal(t, 2, stats.sameSecondEntries)
	require.Equal(t, 1, stats.followEdges)
	require.Equal(t, 1, stats.followerEdges)
	require.Zero(t, stats.missingFollowerEdges)
	require.Zero(t, stats.missingFollowEdges)
	require.Equal(t, 1, stats.maxFollowers)
}

func TestAuditStoreReportsLegacyEntryIndexKey(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	author := uuid.Must(uuid.NewV4())
	entryID := uuid.Must(uuid.NewV4())
	date := time.Now().UTC().Truncate(time.Second)
	entryKey, err := model.PutEntry(db, &pb.Entry{
		Id: entryID.String(), Date: date.Format(time.RFC3339), ProfileUuid: author.String(),
		From: &pb.Feed{Uuid: author.String()},
	})
	require.NoError(t, err)
	// Replace the current-format row with a legacy Flake-shaped row.
	require.NoError(t, model.EntryIndex.RemoveIndex(db, author, date, entryKey))
	legacyKey := store.NewUUIDFlakeKey(model.TableEntryIndex, author, db.TimeTravelReverseId(date)).Bytes()
	require.NoError(t, db.Put(legacyKey, entryKey))

	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.entryIndexes)
	require.Equal(t, 1, stats.noncanonicalIndexes)
	require.Zero(t, stats.orphanIndexes)
	require.Equal(t, 1, stats.missingDirectIndexes)
}

func TestAuditStoreReportsNoncanonicalEntryKey(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	entryID := uuid.Must(uuid.NewV4())
	putLegacyStringKeyEntry(t, db, &pb.Entry{
		Id: entryID.String(), Date: time.Now().UTC().Format(time.RFC3339),
		ProfileUuid: uuid.Must(uuid.NewV4()).String(),
	})
	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.noncanonicalEntries)
	require.Zero(t, stats.entryKeyIDMismatches)
}

func TestAuditStoreFindsTimelineDrift(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	viewer := uuid.Must(uuid.NewV4())
	base := time.Now().UTC().Truncate(time.Millisecond)
	missingPosition := uuid.Must(uuid.NewV4())
	missingIndex := uuid.Must(uuid.NewV4())
	mismatch := uuid.Must(uuid.NewV4())
	missingEntry := uuid.Must(uuid.NewV4())
	duplicate := uuid.Must(uuid.NewV4())
	for _, entry := range []uuid.UUID{missingPosition, missingIndex, mismatch, duplicate} {
		_, err := model.Entry.Put(db, entry.Bytes(), &pb.Entry{Id: entry.String(), Date: base.Format(time.RFC3339), ProfileUuid: viewer.String()})
		require.NoError(t, err)
	}
	for _, entry := range []uuid.UUID{missingPosition, missingIndex, mismatch, missingEntry, duplicate} {
		_, err := model.MoveTimelineEntry(db, viewer, entry, base, nil)
		require.NoError(t, err)
	}
	require.NoError(t, db.Delete(model.TimelinePositionKey(viewer, missingPosition)))
	missingIndexKey, err := model.TimelineIndexKey(viewer, missingIndex, base)
	require.NoError(t, err)
	require.NoError(t, db.Delete(missingIndexKey))
	var wrong [8]byte
	binary.BigEndian.PutUint64(wrong[:], uint64(base.Add(time.Minute).UnixMilli()))
	require.NoError(t, db.Put(model.TimelinePositionKey(viewer, mismatch), wrong[:]))
	duplicateKey, err := model.TimelineIndexKey(viewer, duplicate, base.Add(time.Minute))
	require.NoError(t, err)
	require.NoError(t, db.Put(duplicateKey, nil))

	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.timelineMissingEntry)
	require.Equal(t, 1, stats.timelineMissingPos)
	require.Equal(t, 1, stats.timelineMissingIndex)
	require.Equal(t, 1, stats.timelineDuplicates)
	require.Equal(t, 1, stats.timelineTimeMismatch)
}
