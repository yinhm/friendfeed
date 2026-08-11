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
	orphanIndex := store.NewUUIDFlakeKey(model.TableEntryIndex, orphanOwner, db.NextId())
	require.NoError(t, db.Put(orphanIndex.Bytes(), model.Entry.PrefixAppend(uuid.Must(uuid.NewV4()).Bytes())))

	stats, err := auditStore(db)
	require.NoError(t, err)
	require.Equal(t, 2, stats.entries)
	require.Equal(t, 3, stats.entryIndexes)
	require.Equal(t, 1, stats.missingDirectIndexes)
	require.Equal(t, 1, stats.orphanIndexes)
	require.Equal(t, 3, stats.missingTimeline)
	require.Equal(t, 1, stats.sameSecondGroups)
	require.Equal(t, 2, stats.sameSecondEntries)
	require.Equal(t, 1, stats.followEdges)
	require.Equal(t, 1, stats.followerEdges)
	require.Zero(t, stats.missingFollowerEdges)
	require.Zero(t, stats.missingFollowEdges)
	require.Equal(t, 1, stats.maxFollowers)
}
