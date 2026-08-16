package main

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func TestRebuildInteractionTimelinesRestoresDerivedRows(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	actor := uuid.Must(uuid.NewV4())
	entry := uuid.Must(uuid.NewV4())
	oldComment := uuid.Must(uuid.NewV4())
	latestComment := uuid.Must(uuid.NewV4())
	oldAt := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	latestAt := oldAt.Add(time.Minute)
	putProto := func(key store.Key, message proto.Message) {
		t.Helper()
		raw, marshalErr := proto.Marshal(message)
		require.NoError(t, marshalErr)
		require.NoError(t, db.Put(key, raw))
	}
	putProto(model.Entry.PrefixAppend(entry.Bytes()), &pb.Entry{Id: entry.String()})
	putProto(model.LikeKey(entry, actor), &pb.Like{
		Date: oldAt.Format(time.RFC3339), From: &pb.Feed{Uuid: actor.String()},
	})
	putProto(model.CommentKey(entry, oldComment), &pb.Comment{
		Id: oldComment.String(), Date: oldAt.Format(time.RFC3339), From: &pb.Feed{Uuid: actor.String()},
	})
	putProto(model.CommentKey(entry, latestComment), &pb.Comment{
		Id: latestComment.String(), Date: latestAt.Format(time.RFC3339), From: &pb.Feed{Uuid: actor.String()},
	})

	dry, err := rebuildInteractionTimelines(db, actor.String(), true)
	require.NoError(t, err)
	require.Equal(t, 1, dry.indexedLikes)
	require.Equal(t, 1, dry.indexedComments)
	likeIndex, err := model.LikeTimelineKey(actor, entry, oldAt)
	require.NoError(t, err)
	_, err = db.Get(likeIndex)
	require.ErrorIs(t, err, store.ErrNotFound)

	stats, err := rebuildInteractionTimelines(db, actor.String(), false)
	require.NoError(t, err)
	require.Equal(t, 1, stats.indexedLikes)
	require.Equal(t, 1, stats.indexedComments)
	_, err = db.Get(likeIndex)
	require.NoError(t, err)
	position, err := db.Get(model.CommentTimelinePositionKey(actor, entry))
	require.NoError(t, err)
	positionAt, positionComment, err := model.DecodeCommentTimelinePosition(position)
	require.NoError(t, err)
	require.Equal(t, latestAt, positionAt)
	require.Equal(t, latestComment, positionComment)

	audit := storeAuditStats{}
	require.NoError(t, auditInteractionTimelines(db, &audit))
	require.Zero(t, audit.interactionOrphans)
	require.Zero(t, audit.interactionMismatches)
}
