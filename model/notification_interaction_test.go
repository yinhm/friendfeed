package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func notificationInteractionEntry(t *testing.T, db *store.Store, author uuid.UUID) *pb.Entry {
	t.Helper()
	entry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: author.String(),
		FeedUuid:    author.String(),
		Date:        time.Now().UTC().Format(time.RFC3339),
		// Interaction notification tests do not exercise search indexing. Keep
		// the body empty so PutEntry does not require the process-global search
		// index used by production server startup.
		Body: "",
	}
	_, err := PutEntry(db, entry)
	require.NoError(t, err)
	return entry
}

func TestLikeNotificationCreatedOnceAcrossToggle(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	author := notificationTestUser(t, db, "author")
	actor := notificationTestUser(t, db, "actor")
	entry := notificationInteractionEntry(t, db, author)
	actorProfile, err := GetProfileFromUuid(db, actor)
	require.NoError(t, err)

	_, _, err = PutLike(db, actorProfile, entry)
	require.NoError(t, err)
	entryUUID := uuid.Must(uuid.FromString(entry.Id))
	id, err := NotificationID(NotificationEntryLiked, fmt.Sprintf("%s:%s", entryUUID, actor), author)
	require.NoError(t, err)
	record, err := GetNotification(db, author, id)
	require.NoError(t, err)
	require.Equal(t, NotificationEntryLiked, record.Kind)
	require.Equal(t, actor.String(), record.ActorUUID)
	require.Equal(t, entry.Id, record.EntryUUID)

	state, err := GetNotificationState(db, author)
	require.NoError(t, err)
	require.Equal(t, uint32(1), state.TotalCount)

	_, err = DeleteLike(db, actorProfile, entry)
	require.NoError(t, err)
	_, _, err = PutLike(db, actorProfile, entry)
	require.NoError(t, err)
	state, err = GetNotificationState(db, author)
	require.NoError(t, err)
	require.Equal(t, uint32(1), state.TotalCount, "unlike/re-like must reuse the deterministic Like notification")
}

func TestCommentNotificationOnlyForNewComment(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	author := notificationTestUser(t, db, "author")
	actor := notificationTestUser(t, db, "commenter")
	entry := notificationInteractionEntry(t, db, author)
	actorProfile, err := GetProfileFromUuid(db, actor)
	require.NoError(t, err)
	commentID := uuid.Must(uuid.NewV4())

	_, _, err = PutComment(db, actorProfile, entry, &pb.Comment{Id: commentID.String(), Body: "first"})
	require.NoError(t, err)
	id, err := NotificationID(NotificationEntryCommented, commentID.String(), author)
	require.NoError(t, err)
	record, err := GetNotification(db, author, id)
	require.NoError(t, err)
	require.Equal(t, commentID.String(), record.CommentUUID)

	_, _, err = PutComment(db, actorProfile, entry, &pb.Comment{Id: commentID.String(), Body: "edited"})
	require.NoError(t, err)
	state, err := GetNotificationState(db, author)
	require.NoError(t, err)
	require.Equal(t, uint32(1), state.TotalCount, "editing an existing comment must not emit another notification")
}

func TestInteractionNotificationSkipsSelfAndGroupRecipient(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	author := notificationTestUser(t, db, "author")
	authorProfile, err := GetProfileFromUuid(db, author)
	require.NoError(t, err)
	selfEntry := notificationInteractionEntry(t, db, author)
	_, _, err = PutLike(db, authorProfile, selfEntry)
	require.NoError(t, err)
	state, err := GetNotificationState(db, author)
	require.NoError(t, err)
	require.Zero(t, state.TotalCount)

	group := uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{
		Uuid: group.String(), Id: "group", Name: "Group", Type: "group",
	}))
	actor := notificationTestUser(t, db, "actor")
	actorProfile, err := GetProfileFromUuid(db, actor)
	require.NoError(t, err)
	groupEntry := &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), ProfileUuid: group.String(), FeedUuid: group.String(),
		Date: time.Now().UTC().Format(time.RFC3339),
	}
	_, err = PutEntry(db, groupEntry)
	require.NoError(t, err)
	_, _, err = PutComment(db, actorProfile, groupEntry, &pb.Comment{Id: uuid.Must(uuid.NewV4()).String(), Body: "comment"})
	require.NoError(t, err)
	groupState, err := GetNotificationState(db, group)
	require.NoError(t, err)
	require.Zero(t, groupState.TotalCount)
}
