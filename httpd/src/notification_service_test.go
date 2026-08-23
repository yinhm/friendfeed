package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNotificationToViewLinksFailedServiceWithoutSourceDetails(t *testing.T) {
	record := notificationRecordDTO{
		Kind:               "FEED_SERVICE_FAILED",
		TargetUUID:         "00000000-0000-4000-8000-000000000123",
		TargetID:           "book-club",
		TargetNameSnapshot: "Book Club",
		ActivityAtMS:       1,
	}
	view := notificationToView(record)
	require.Equal(t, "An imported service for Book Club needs attention", view.Text)
	require.Equal(t, "/feed/book-club/import", view.Href)
	require.NotContains(t, view.Text, "http")
	require.NotContains(t, view.Text, "?")
	require.NotContains(t, view.Text, "error")
}
