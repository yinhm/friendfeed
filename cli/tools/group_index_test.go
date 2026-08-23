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

func TestRebuildGroupIndexDryRunAndApply(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	creator := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{Uuid: creator.String(), Id: "creator", Type: "user"}))
	group, err := model.CreateGroup(db, creator, "indexed-group", "Indexed", "", "", false, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	groupUUID := uuid.Must(uuid.FromString(group.Uuid))

	iter, err := db.NewIterator(model.GroupIndex.Prefix)
	require.NoError(t, err)
	require.True(t, iter.First())
	staleKey := iter.Key()
	require.NoError(t, iter.Close())
	require.NoError(t, db.Delete(staleKey))

	stats, err := rebuildGroupIndex(db, "indexed-group", true)
	require.NoError(t, err)
	require.Equal(t, 1, stats.changed)
	_, count, err := model.GroupIndexActivity(db, groupUUID)
	require.NoError(t, err)
	require.Zero(t, count)

	stats, err = rebuildGroupIndex(db, "indexed-group", false)
	require.NoError(t, err)
	require.Equal(t, 1, stats.changed)
	activity, count, err := model.GroupIndexActivity(db, groupUUID)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, time.Unix(0, 0).UTC(), activity)
	stats, err = rebuildGroupIndex(db, "", false)
	require.NoError(t, err)
	require.Equal(t, 1, stats.profiles)
	require.Equal(t, 1, stats.indexed)
	require.Zero(t, stats.changed)

	audit, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 1, audit.groupIndexRows)
	require.Zero(t, audit.missingGroupIndexRows)
	require.Zero(t, audit.orphanGroupIndexRows)
}

func TestRebuildGroupIndexUsesLatestEntry(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	creator := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{Uuid: creator.String(), Id: "creator2", Type: "user"}))
	group, err := model.CreateGroup(db, creator, "active-index", "Active", "", "", false, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	want := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	_, err = model.PutEntry(db, &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), ProfileUuid: creator.String(), FeedUuid: group.Uuid,
		Date: want.Format(time.RFC3339),
	})
	require.NoError(t, err)

	stats, err := rebuildGroupIndex(db, "active-index", false)
	require.NoError(t, err)
	require.Equal(t, 1, stats.changed) // runtime position uses commit time
	got, count, err := model.GroupIndexActivity(db, uuid.Must(uuid.FromString(group.Uuid)))
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, want, got)
}
