package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

type observedTimelineMove struct {
	viewer uuid.UUID
	entry  uuid.UUID
	kind   TimelineActivityKind
	at     time.Time
}

func TestFanoutTimelineObserverSeesOnlyCommittedMoves(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	author := uuid.Must(uuid.NewV4())
	follower := uuid.Must(uuid.NewV4())
	inactive := uuid.Must(uuid.NewV4())
	entryID := uuid.Must(uuid.NewV4())
	require.NoError(t, TouchTimelineState(db, author, now))
	require.NoError(t, TouchTimelineState(db, follower, now))
	require.NoError(t, db.Put(NewKeyFrom(Follower.Prefix, author.Bytes(), follower.Bytes()), []byte("1")))
	require.NoError(t, db.Put(NewKeyFrom(Follower.Prefix, author.Bytes(), inactive.Bytes()), []byte("1")))

	published := now.Add(-time.Hour)
	entry := &pb.Entry{
		Id:          entryID.String(),
		ProfileUuid: author.String(),
		Date:        published.Format(time.RFC3339Nano),
	}
	var got []observedTimelineMove
	observer := func(viewer, entry uuid.UUID, kind TimelineActivityKind, at time.Time) {
		got = append(got, observedTimelineMove{viewer: viewer, entry: entry, kind: kind, at: at})
	}

	moved, err := FanoutTimelineActivity(db, entry, published, TimelineActivityPublish, observer)
	require.NoError(t, err)
	require.Equal(t, 2, moved) // author + active follower; inactive is skipped
	require.Len(t, got, 2)
	require.ElementsMatch(t, []uuid.UUID{author, follower}, []uuid.UUID{got[0].viewer, got[1].viewer})
	for _, event := range got {
		require.Equal(t, entryID, event.entry)
		require.Equal(t, TimelineActivityPublish, event.kind)
		require.Equal(t, published, event.at)
	}

	got = nil
	moved, err = FanoutTimelineActivity(db, entry, published.Add(-time.Minute), TimelineActivityComment, observer)
	require.NoError(t, err)
	require.Zero(t, moved)
	require.Empty(t, got, "older activity must not emit a hint")
}

func TestFanoutTimelineObserverRespectsLikeCooldown(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	author := uuid.Must(uuid.NewV4())
	entryID := uuid.Must(uuid.NewV4())
	require.NoError(t, TouchTimelineState(db, author, now))
	published := now.Add(-time.Hour)
	entry := &pb.Entry{
		Id:          entryID.String(),
		ProfileUuid: author.String(),
		Date:        published.Format(time.RFC3339Nano),
	}
	_, err = FanoutTimelineActivity(db, entry, published, TimelineActivityPublish, nil)
	require.NoError(t, err)

	calls := 0
	observer := func(uuid.UUID, uuid.UUID, TimelineActivityKind, time.Time) { calls++ }
	moved, err := FanoutTimelineActivity(db, entry, published.Add(5*time.Minute), TimelineActivityLike, observer)
	require.NoError(t, err)
	require.Zero(t, moved)
	require.Zero(t, calls)

	moved, err = FanoutTimelineActivity(db, entry, published.Add(11*time.Minute), TimelineActivityLike, observer)
	require.NoError(t, err)
	require.Equal(t, 1, moved)
	require.Equal(t, 1, calls)
}
