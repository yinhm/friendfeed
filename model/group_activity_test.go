package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestGroupActivityTracksMutationsAndRebuilds(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	user := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{Uuid: user.String(), Id: "active-user", Name: "Active", Type: "user"}
	require.NoError(t, UpdateProfile(db, profile))
	createdAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	group, err := CreateGroup(db, user, "active-group", "Active Group", "", "", false, createdAt)
	require.NoError(t, err)
	groupUUID := uuid.Must(uuid.FromString(group.Uuid))
	indexActivity := func() time.Time {
		t.Helper()
		iter, err := db.NewIterator(GroupIndex.Prefix)
		require.NoError(t, err)
		defer iter.Close()
		require.True(t, iter.First())
		indexedGroup, activity, err := ParseGroupIndexKey(iter.Key())
		require.NoError(t, err)
		require.Equal(t, groupUUID, indexedGroup)
		iter.Next()
		require.False(t, iter.Valid())
		require.NoError(t, iter.Error())
		return activity
	}
	require.Equal(t, createdAt, indexActivity())

	assertScore := func(want int64) {
		t.Helper()
		rows, err := GetGroupActivity(db, user)
		require.NoError(t, err)
		require.Equal(t, []GroupActivity{{GroupUUID: groupUUID.String(), Score: want}}, rows)
	}
	assertScore(GroupActivityCreateScore)

	entry := &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), Date: time.Now().UTC().Format(time.RFC3339),
		ProfileUuid: user.String(), FeedUuid: groupUUID.String(),
	}
	_, err = PutEntry(db, entry)
	require.NoError(t, err)
	assertScore(110)
	entryActivity := indexActivity()
	require.True(t, entryActivity.After(createdAt))

	_, entry, err = PutLike(db, profile, entry)
	require.NoError(t, err)
	assertScore(113)
	require.False(t, indexActivity().Before(entryActivity))
	commentID := uuid.Must(uuid.NewV4()).String()
	_, entry, err = PutComment(db, profile, entry, &pb.Comment{Id: commentID, Body: "hello"})
	require.NoError(t, err)
	assertScore(117)

	entry, err = DeleteComment(db, profile, entry, commentID)
	require.NoError(t, err)
	assertScore(113)
	entry, err = DeleteLike(db, profile, entry)
	require.NoError(t, err)
	assertScore(110)
	require.NoError(t, DeleteEntry(db, entry.Id))
	assertScore(100)

	rebuilt, err := RebuildGroupActivityForUser(db, user)
	require.NoError(t, err)
	require.Equal(t, []GroupActivity{{GroupUUID: groupUUID.String(), Score: 100}}, rebuilt)
}

func TestJoinGroupMaterializesExistingActivity(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	creator := uuid.Must(uuid.NewV4())
	visitor := uuid.Must(uuid.NewV4())
	creatorProfile := &pb.Profile{Uuid: creator.String(), Id: "creator", Type: "user"}
	visitorProfile := &pb.Profile{Uuid: visitor.String(), Id: "visitor", Type: "user"}
	require.NoError(t, UpdateProfile(db, creatorProfile))
	require.NoError(t, UpdateProfile(db, visitorProfile))
	group, err := CreateGroup(db, creator, "join-activity", "Join Activity", "", "", false, time.Now().UTC())
	require.NoError(t, err)
	groupUUID := uuid.Must(uuid.FromString(group.Uuid))
	entry := &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), Date: time.Now().UTC().Format(time.RFC3339),
		ProfileUuid: creator.String(), FeedUuid: group.Uuid,
	}
	_, err = PutEntry(db, entry)
	require.NoError(t, err)
	_, entry, err = PutLike(db, visitorProfile, entry)
	require.NoError(t, err)
	_, _, err = PutComment(db, visitorProfile, entry, &pb.Comment{Id: uuid.Must(uuid.NewV4()).String()})
	require.NoError(t, err)
	_, err = GetGroupActivity(db, visitor)
	require.ErrorIs(t, err, store.ErrNotFound)

	require.NoError(t, JoinGroup(db, groupUUID, visitor))
	rows, err := GetGroupActivity(db, visitor)
	require.NoError(t, err)
	require.Equal(t, []GroupActivity{{GroupUUID: group.Uuid, Score: 7}}, rows)
}

// A user whose Follow edges point only at non-Group feeds has no ranking by
// definition; the rebuild must return nil without touching the heavy scans.
func TestRebuildGroupActivitySkipsUsersWithoutGroupMembership(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	user := uuid.Must(uuid.NewV4())
	other := uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{Uuid: user.String(), Id: "memberless", Type: "user"}))
	require.NoError(t, UpdateProfile(db, &pb.Profile{Uuid: other.String(), Id: "plain-feed", Type: "user"}))
	require.NoError(t, db.Set(NewKeyFrom(Follow.Prefix, user.Bytes(), other.Bytes()), []byte("1")))

	rows, err := RebuildGroupActivityForUser(db, user)
	require.NoError(t, err)
	require.Nil(t, rows)
}
