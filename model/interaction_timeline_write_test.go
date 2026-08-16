package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestLikeMaintainsInteractionTimeline(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)
	entry := newLikeTestEntry()
	entryID := uuid.Must(uuid.FromString(entry.Id))

	_, entry, err := PutLike(db, owner, entry)
	require.NoError(t, err)
	require.Len(t, entry.Likes, 1)
	created := timeMustParse(t, entry.Likes[0].Date)
	index, err := LikeTimelineKey(likeTestOwnerUUID, entryID, created)
	require.NoError(t, err)
	_, err = db.Get(index)
	require.NoError(t, err)

	entry, err = DeleteLike(db, owner, entry)
	require.NoError(t, err)
	require.Empty(t, entry.Likes)
	_, err = db.Get(index)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestCommentTimelineCollapsesEditsAndFallsBackOnDelete(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)
	entry := newLikeTestEntry()
	entryID := uuid.Must(uuid.FromString(entry.Id))
	firstID := uuid.Must(uuid.NewV4())
	secondID := uuid.Must(uuid.NewV4())

	_, entry, err := PutComment(db, owner, entry, &pb.Comment{Id: firstID.String(), Body: "first"})
	require.NoError(t, err)
	_, entry, err = PutComment(db, owner, entry, &pb.Comment{Id: secondID.String(), Body: "second"})
	require.NoError(t, err)

	positionKey := CommentTimelinePositionKey(likeTestOwnerUUID, entryID)
	position, err := db.Get(positionKey)
	require.NoError(t, err)
	latestAt, latestID, err := DecodeCommentTimelinePosition(position)
	require.NoError(t, err)
	require.Equal(t, secondID, latestID)
	requireInteractionRowCount(t, db, CommentTimelinePrefix(likeTestOwnerUUID), 1)

	_, entry, err = PutComment(db, owner, entry, &pb.Comment{Id: secondID.String(), Body: "edited"})
	require.NoError(t, err)
	afterEdit, err := db.Get(positionKey)
	require.NoError(t, err)
	require.Equal(t, position, afterEdit, "editing must not move the comment timeline")

	entry, err = DeleteComment(db, owner, entry, secondID.String())
	require.NoError(t, err)
	position, err = db.Get(positionKey)
	require.NoError(t, err)
	fallbackAt, fallbackID, err := DecodeCommentTimelinePosition(position)
	require.NoError(t, err)
	require.Equal(t, firstID, fallbackID)
	require.False(t, fallbackAt.After(latestAt))
	requireInteractionRowCount(t, db, CommentTimelinePrefix(likeTestOwnerUUID), 1)

	_, err = DeleteComment(db, owner, entry, firstID.String())
	require.NoError(t, err)
	_, err = db.Get(positionKey)
	require.ErrorIs(t, err, store.ErrNotFound)
	requireInteractionRowCount(t, db, CommentTimelinePrefix(likeTestOwnerUUID), 0)
}

func requireInteractionRowCount(t *testing.T, db *store.Store, prefix store.Key, want int) {
	t.Helper()
	got, err := db.ForwardScan(prefix, func(_ int, _, _ []byte) error { return nil })
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func timeMustParse(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, raw)
	require.NoError(t, err)
	return parsed
}
