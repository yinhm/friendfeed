package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNotificationToViewUsesGroupWorkflowLinks(t *testing.T) {
	received := notificationToView(notificationRecordDTO{
		Kind:               "FOLLOW_REQUEST_RECEIVED",
		RecipientUUID:      "admin",
		TargetUUID:         "group-uuid",
		ActorNameSnapshot:  "Alice",
		TargetNameSnapshot: "Book Club",
		TargetType:         "group",
		TargetID:           "book-club",
	})
	require.Equal(t, "Alice requested to join Book Club", received.Text)
	require.Equal(t, "/groups/book-club/members", received.Href)

	approved := notificationToView(notificationRecordDTO{
		Kind:               "FOLLOW_REQUEST_APPROVED",
		TargetNameSnapshot: "Book Club",
		TargetType:         "group",
		TargetID:           "book-club",
	})
	require.Equal(t, "Your request to join Book Club was approved", approved.Text)
	require.Equal(t, "/feed/book-club", approved.Href)

	rejected := notificationToView(notificationRecordDTO{
		Kind:               "FOLLOW_REQUEST_REJECTED",
		TargetNameSnapshot: "Book Club",
		TargetType:         "group",
		TargetID:           "book-club",
	})
	require.Equal(t, "Your request to join Book Club was declined", rejected.Text)
	require.Equal(t, "/feed/book-club", rejected.Href)
}

func TestNotificationToViewUsesUserFeedLinks(t *testing.T) {
	received := notificationToView(notificationRecordDTO{
		Kind:               "FOLLOW_REQUEST_RECEIVED",
		RecipientUUID:      "owner-uuid",
		TargetUUID:         "owner-uuid",
		ActorNameSnapshot:  "Alice",
		TargetNameSnapshot: "Owner",
		TargetType:         "user",
		TargetID:           "owner",
	})
	require.Equal(t, "Alice requested to follow you", received.Text)
	require.Equal(t, "/account/requests", received.Href)

	approved := notificationToView(notificationRecordDTO{
		Kind:               "FOLLOW_REQUEST_APPROVED",
		TargetNameSnapshot: "Owner",
		TargetType:         "user",
		TargetID:           "owner",
	})
	require.Equal(t, "Your request to follow Owner was approved", approved.Text)
	require.Equal(t, "/feed/owner", approved.Href)
}

func TestNotificationToViewLinksEntryAndGroupTransitions(t *testing.T) {
	liked := notificationToView(notificationRecordDTO{
		Kind:              "ENTRY_LIKED",
		ActorNameSnapshot: "Alice",
		EntryUUID:         "entry-id",
	})
	require.Equal(t, "/e/entry-id", liked.Href)

	removed := notificationToView(notificationRecordDTO{
		Kind:               "GROUP_MEMBER_REMOVED",
		ActorNameSnapshot:  "Admin",
		TargetNameSnapshot: "Book Club",
		TargetID:           "book-club",
	})
	require.Equal(t, "Admin removed you from Book Club", removed.Text)
	require.Equal(t, "/feed/book-club", removed.Href)
}
