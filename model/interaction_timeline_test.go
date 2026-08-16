package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/store"
)

func TestInteractionTimelineKeyRoundTrip(t *testing.T) {
	actor, entry, comment := uuid.Must(uuid.NewV4()), uuid.Must(uuid.NewV4()), uuid.Must(uuid.NewV4())
	at := time.Date(2026, 8, 16, 1, 2, 3, 4_000_000, time.UTC)
	likeKey, err := LikeTimelineKey(actor, entry, at)
	require.NoError(t, err)
	commentKey, err := CommentTimelineKey(actor, entry, at)
	require.NoError(t, err)
	for _, table := range []struct {
		prefix store.KeyPrefix
		key    []byte
	}{
		{TableLikeTimeline, likeKey},
		{TableCommentTimeline, commentKey},
	} {
		gotActor, gotEntry, gotAt, err := ParseInteractionTimelineKey(table.key, table.prefix)
		require.NoError(t, err)
		require.Equal(t, actor, gotActor)
		require.Equal(t, entry, gotEntry)
		require.Equal(t, at, gotAt)
	}
	raw, err := EncodeCommentTimelinePosition(at, comment)
	require.NoError(t, err)
	gotAt, gotComment, err := DecodeCommentTimelinePosition(raw)
	require.NoError(t, err)
	require.Equal(t, at, gotAt)
	require.Equal(t, comment, gotComment)
}
