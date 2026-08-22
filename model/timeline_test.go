package model

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestMoveTimelineEntryIsMonotonicAndOrdered(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	viewer := uuid.Must(uuid.NewV4())
	first := uuid.Must(uuid.NewV4())
	second := uuid.Must(uuid.NewV4())
	base := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	_, err = MoveTimelineEntry(db, viewer, first, base, nil)
	require.NoError(t, err)
	moved, err := MoveTimelineEntry(db, viewer, first, base.Add(-time.Minute), nil)
	require.NoError(t, err)
	require.False(t, moved)
	_, err = MoveTimelineEntry(db, viewer, first, base.Add(time.Hour), nil)
	require.NoError(t, err)
	_, err = MoveTimelineEntry(db, viewer, second, base.Add(2*time.Hour), nil)
	require.NoError(t, err)

	var entries []uuid.UUID
	_, err = db.ForwardScan(TimelineIndexPrefix(viewer), func(_ int, key, _ []byte) error {
		_, entry, _, err := ParseTimelineIndexKey(key)
		entries = append(entries, entry)
		return err
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{second, first}, entries)
	position, err := TimelinePositionTime(db, viewer, first)
	require.NoError(t, err)
	require.Equal(t, base.Add(time.Hour), position)
}

func TestTimelineStateLifecycle(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	viewer := uuid.Must(uuid.NewV4())
	now := time.Date(2026, 8, 13, 10, 0, 0, 123000000, time.UTC)
	active, err := TimelineIsActive(db, viewer, now)
	require.NoError(t, err)
	require.False(t, active)

	require.NoError(t, TouchTimelineState(db, viewer, now))
	last, err := TimelineLastAccess(db, viewer)
	require.NoError(t, err)
	require.Equal(t, now.Truncate(time.Millisecond), last)

	active, err = TimelineIsActive(db, viewer, now.Add(TimelineActiveFor))
	require.NoError(t, err)
	require.True(t, active)
	active, err = TimelineIsActive(db, viewer, now.Add(TimelineActiveFor+time.Millisecond))
	require.NoError(t, err)
	require.False(t, active)

	require.NoError(t, TouchTimelineState(db, viewer, now.Add(time.Hour)))
	last, err = TimelineLastAccess(db, viewer)
	require.NoError(t, err)
	require.Equal(t, now.Add(time.Hour).Truncate(time.Millisecond), last)
	require.NoError(t, DeleteTimelineState(db, viewer))
	_, err = TimelineLastAccess(db, viewer)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestTimelineStateRejectsInvalidData(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	require.Error(t, TouchTimelineState(db, uuid.Nil, time.Now()))
	require.Error(t, TouchTimelineState(db, uuid.Must(uuid.NewV4()), time.Unix(-1, 0)))
	require.Error(t, DeleteTimelineState(db, uuid.Nil))
	_, err = TimelineLastAccess(db, uuid.Nil)
	require.Error(t, err)

	viewer := uuid.Must(uuid.NewV4())
	require.NoError(t, db.Set(TimelineStateKey(viewer), []byte{1}))
	_, err = TimelineLastAccess(db, viewer)
	require.ErrorContains(t, err, "invalid TimelineState value length")

	var overflow [8]byte
	binary.BigEndian.PutUint64(overflow[:], ^uint64(0))
	require.NoError(t, db.Set(TimelineStateKey(viewer), overflow[:]))
	_, err = TimelineLastAccess(db, viewer)
	require.ErrorContains(t, err, "overflows int64")
}

func TestParseTimelineLastAccess(t *testing.T) {
	at := time.Date(2026, 8, 13, 10, 0, 0, 123000000, time.UTC)
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(at.UnixMilli()))

	got, err := ParseTimelineLastAccess(raw[:])
	require.NoError(t, err)
	require.Equal(t, at, got)
	_, err = ParseTimelineLastAccess(raw[:7])
	require.ErrorContains(t, err, "invalid TimelineState value length")
}

func TestMoveTimelineEntryAppliesPerEntryCooldown(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	viewer := uuid.Must(uuid.NewV4())
	entry := uuid.Must(uuid.NewV4())
	base := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	_, err = MoveTimelineEntry(db, viewer, entry, base, nil)
	require.NoError(t, err)

	likeAt := base.Add(5 * time.Minute)
	moved, err := MoveTimelineEntry(db, viewer, entry, likeAt, func(old time.Time, exists bool) bool {
		return !exists || likeAt.Sub(old) >= LikeBumpCooldown
	})
	require.NoError(t, err)
	require.False(t, moved)
	likeAt = base.Add(11 * time.Minute)
	moved, err = MoveTimelineEntry(db, viewer, entry, likeAt, func(old time.Time, exists bool) bool {
		return !exists || likeAt.Sub(old) >= LikeBumpCooldown
	})
	require.NoError(t, err)
	require.True(t, moved)
}

func TestTrimHomeTimelineDeletesIndexAndPosition(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	viewer := uuid.Must(uuid.NewV4())
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	entries := make([]uuid.UUID, 3)
	for i := range entries {
		entries[i] = uuid.Must(uuid.NewV4())
		_, err := MoveTimelineEntry(db, viewer, entries[i], now.Add(-time.Duration(i)*time.Hour), nil)
		require.NoError(t, err)
	}
	deleted, err := TrimHomeTimeline(db, viewer, 2, TimelineRetentionMax, now)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	count, err := db.ForwardScan(TimelineIndexPrefix(viewer), func(int, []byte, []byte) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 2, count)
	_, err = TimelinePositionTime(db, viewer, entries[2])
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestReplaceHomeTimelineFailurePreservesOldCache(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	viewer := uuid.Must(uuid.NewV4())
	oldEntry := uuid.Must(uuid.NewV4())
	oldActivity := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	_, err = MoveTimelineEntry(db, viewer, oldEntry, oldActivity, nil)
	require.NoError(t, err)

	err = ReplaceHomeTimeline(db, viewer, map[uuid.UUID]time.Time{
		uuid.Must(uuid.NewV4()): time.Unix(-1, 0),
	})
	require.ErrorContains(t, err, "before Unix epoch")

	rows := make([]uuid.UUID, 0, 1)
	_, err = db.ForwardScan(TimelineIndexPrefix(viewer), func(_ int, key, _ []byte) error {
		_, entry, _, parseErr := ParseTimelineIndexKey(key)
		rows = append(rows, entry)
		return parseErr
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{oldEntry}, rows)
	position, err := TimelinePositionTime(db, viewer, oldEntry)
	require.NoError(t, err)
	require.Equal(t, oldActivity, position)
}

func TestFanoutTimelineActivityBumpsFollowersButNotDirectFeed(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	author := uuid.Must(uuid.NewV4())
	follower := uuid.Must(uuid.NewV4())
	inactiveFollower := uuid.Must(uuid.NewV4())
	entryID := uuid.Must(uuid.NewV4())
	require.NoError(t, db.Put(NewKeyFrom(Follower.Prefix, author.Bytes(), follower.Bytes()), []byte("1")))
	require.NoError(t, db.Put(NewKeyFrom(Follower.Prefix, author.Bytes(), inactiveFollower.Bytes()), []byte("1")))
	require.NoError(t, TouchTimelineState(db, author, time.Now().UTC()))
	require.NoError(t, TouchTimelineState(db, follower, time.Now().UTC()))
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	entry := &pb.Entry{Id: entryID.String(), ProfileUuid: author.String(), Date: base.Format(time.RFC3339)}
	_, err = PutEntry(db, entry)
	require.NoError(t, err)

	commentAt := base.Add(30 * time.Minute)
	_, err = FanoutTimelineActivity(db, entry, commentAt, TimelineActivityComment, nil)
	require.NoError(t, err)
	for _, viewer := range []uuid.UUID{author, follower} {
		position, err := TimelinePositionTime(db, viewer, entryID)
		require.NoError(t, err)
		require.Equal(t, commentAt, position)
	}
	_, err = TimelinePositionTime(db, inactiveFollower, entryID)
	require.ErrorIs(t, err, store.ErrNotFound)

	// The author's direct feed remains keyed by the publish time and contains
	// exactly one row after the activity bump.
	n, err := db.ForwardScan(store.NewUUIDKey(TableEntryIndex, author).Bytes(), func(int, []byte, []byte) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
