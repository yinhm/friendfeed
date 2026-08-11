package main

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestRebuildEntryIndexesRestoresSourceDerivedRows(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	author := uuid.Must(uuid.NewV4())
	follower := uuid.Must(uuid.NewV4())
	require.NoError(t, db.Put(model.NewKeyFrom(model.Follow.Prefix, follower.Bytes(), author.Bytes()), []byte("1")))
	require.NoError(t, db.Put(model.NewKeyFrom(model.Follower.Prefix, author.Bytes(), follower.Bytes()), []byte("1")))
	date := time.Now().UTC().Truncate(time.Second)
	entryKeys := make([]store.Key, 0, 2)
	for range 2 {
		entryID := uuid.Must(uuid.NewV4())
		key, err := model.PutEntry(db, &pb.Entry{
			Id: entryID.String(), Date: date.Format(time.RFC3339), ProfileUuid: author.String(),
			From: &pb.Feed{Uuid: author.String(), Id: "author"},
		})
		require.NoError(t, err)
		entryKeys = append(entryKeys, key)
	}
	require.NoError(t, model.EntryIndex.RemoveIndex(db, author, date, entryKeys[0]))
	orphan := store.NewUUIDFlakeKey(model.TableEntryIndex, uuid.Must(uuid.NewV4()), db.NextId())
	require.NoError(t, db.Put(orphan.Bytes(), model.Entry.PrefixAppend(uuid.Must(uuid.NewV4()).Bytes())))

	dry, err := rebuildEntryIndexes(db, entryIndexRebuildOptions{dryRun: true})
	require.NoError(t, err)
	require.Equal(t, 2, dry.entries)
	require.Equal(t, 2, dry.direct)
	require.Equal(t, 4, dry.timeline)
	require.Zero(t, dry.removed)

	stats, err := rebuildEntryIndexes(db, entryIndexRebuildOptions{})
	require.NoError(t, err)
	require.Equal(t, 6, stats.removed)
	audit, err := auditStore(db)
	require.NoError(t, err)
	require.Zero(t, audit.missingDirectIndexes)
	require.Zero(t, audit.missingTimeline)
	require.Zero(t, audit.orphanIndexes)
	require.Equal(t, 6, audit.entryIndexes)
}
