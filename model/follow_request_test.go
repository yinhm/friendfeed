package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func newPrivateUserFeed(t *testing.T, db *store.Store, id string) uuid.UUID {
	t.Helper()
	feed := uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{
		Uuid:    feed.String(),
		Id:      id,
		Name:    id,
		Type:    "user",
		Private: true,
	}))
	return feed
}

func TestFollowRequestKey(t *testing.T) {
	target := uuid.Must(uuid.NewV4())
	requester := uuid.Must(uuid.NewV4())
	key, err := FollowRequestKey(target, requester)
	require.NoError(t, err)
	require.Equal(t, append(append(KeyPrefixToBytes(TableFollowRequest), target.Bytes()...), requester.Bytes()...), []byte(key))

	_, err = FollowRequestKey(uuid.Nil, requester)
	require.Error(t, err)
	_, err = FollowRequestKey(target, uuid.Nil)
	require.Error(t, err)
}

func TestRequestFollowRequiresPrivateTarget(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	publicFeed := newGroupUser(t, db, "public-feed")
	requester := newGroupUser(t, db, "requester")

	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRequestFollow(db, batch, publicFeed, requester, time.Now())
	})
	require.ErrorIs(t, err, ErrFollowTargetNotPrivate)
}

func TestRequestFollowLifecycle(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	feed := newPrivateUserFeed(t, db, "private-feed")
	requester := newGroupUser(t, db, "requester")
	now := time.Now()

	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRequestFollow(db, batch, feed, requester, now)
	}))
	pending, err := IsFollowRequestPending(db, feed, requester)
	require.NoError(t, err)
	require.True(t, pending)

	// Idempotent: a second request keeps the original timestamp.
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRequestFollow(db, batch, feed, requester, now.Add(time.Hour))
	}))
	key, err := FollowRequestKey(feed, requester)
	require.NoError(t, err)
	value, err := db.Get(key)
	require.NoError(t, err)
	require.Equal(t, now.UTC().Format(time.RFC3339Nano), string(value))

	// Cancel is idempotent too.
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageDeleteFollowRequest(db, batch, feed, requester)
	}))
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageDeleteFollowRequest(db, batch, feed, requester)
	}))
	pending, err = IsFollowRequestPending(db, feed, requester)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestRequestFollowReRequestWithinSameSecondGetsNewOccurrence(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	feed := newPrivateUserFeed(t, db, "private-feed")
	requester := newGroupUser(t, db, "requester")
	first := time.Date(2026, 8, 21, 10, 0, 0, 100, time.UTC)
	second := first.Add(200 * time.Nanosecond)

	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRequestFollow(db, batch, feed, requester, first)
	}))
	firstValue, err := FollowRequestRequestedAt(db, feed, requester)
	require.NoError(t, err)
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageDeleteFollowRequest(db, batch, feed, requester)
	}))
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRequestFollow(db, batch, feed, requester, second)
	}))
	secondValue, err := FollowRequestRequestedAt(db, feed, requester)
	require.NoError(t, err)

	require.NotEqual(t, firstValue, secondValue)
	require.Equal(t, first.UTC().Format(time.RFC3339Nano), firstValue)
	require.Equal(t, second.UTC().Format(time.RFC3339Nano), secondValue)
}

func TestApproveFollowRequestUserFeed(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	feed := newPrivateUserFeed(t, db, "private-feed")
	requester := newGroupUser(t, db, "requester")

	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRequestFollow(db, batch, feed, requester, time.Now())
	}))
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageApproveFollowRequest(db, batch, feed, requester)
	}))

	following, err := IsFollower(db, feed, requester)
	require.NoError(t, err)
	require.True(t, following)
	followerExists, err := db.Exists(NewKeyFrom(Follower.Prefix, feed.Bytes(), requester.Bytes()))
	require.NoError(t, err)
	require.True(t, followerExists)

	pending, err := IsFollowRequestPending(db, feed, requester)
	require.NoError(t, err)
	require.False(t, pending)

	// Approving again without a request but with an edge is idempotent.
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageApproveFollowRequest(db, batch, feed, requester)
	}))
}

func TestApproveFollowRequestGroup(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group, _ := setupGroupWithCreator(t, db)
	groupProfile, err := GetProfileFromUuid(db, group)
	require.NoError(t, err)
	groupProfile.Private = true
	require.NoError(t, UpdateProfile(db, groupProfile))

	requester := newGroupUser(t, db, "requester")
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRequestFollow(db, batch, group, requester, time.Now())
	}))
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageApproveFollowRequest(db, batch, group, requester)
	}))

	isMember, err := IsGroupMember(db, group, requester)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestApproveFollowRequestNotFound(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	feed := newPrivateUserFeed(t, db, "private-feed")
	requester := newGroupUser(t, db, "requester")

	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageApproveFollowRequest(db, batch, feed, requester)
	})
	require.ErrorIs(t, err, ErrFollowRequestNotFound)
}

func TestRequestFollowSkippedForExistingFollower(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	feed := newPrivateUserFeed(t, db, "private-feed")
	requester := newGroupUser(t, db, "requester")
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return stageFollowEdges(db, batch, feed, requester)
	}))

	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRequestFollow(db, batch, feed, requester, time.Now())
	}))
	pending, err := IsFollowRequestPending(db, feed, requester)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestListFollowRequestsPagination(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	feed := newPrivateUserFeed(t, db, "private-feed")
	requesters := make([]uuid.UUID, 0, 5)
	for i := range 5 {
		requester := newGroupUser(t, db, fmt.Sprintf("requester-%d", i))
		requesters = append(requesters, requester)
		require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
			return StageRequestFollow(db, batch, feed, requester, time.Now())
		}))
	}

	page1, cursor, err := ListFollowRequests(db, feed, 2, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, cursor)

	seen := map[uuid.UUID]bool{page1[0].Requester: true, page1[1].Requester: true}
	total := len(page1)
	for cursor != "" {
		cursorUUID, err := uuid.FromString(cursor)
		require.NoError(t, err)
		var page []FollowRequestEntry
		page, cursor, err = ListFollowRequests(db, feed, 2, cursorUUID)
		require.NoError(t, err)
		for _, entry := range page {
			require.False(t, seen[entry.Requester], "requester returned twice")
			seen[entry.Requester] = true
		}
		total += len(page)
	}
	require.Equal(t, 5, total)
}

func TestSoftDeleteProfileClearsTargetRequests(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	feed := newPrivateUserFeed(t, db, "private-feed")
	requester := newGroupUser(t, db, "requester")
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRequestFollow(db, batch, feed, requester, time.Now())
	}))

	profile, err := GetProfileFromUuid(db, feed)
	require.NoError(t, err)
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageSoftDeleteProfile(db, batch, profile)
	}))

	pending, err := IsFollowRequestPending(db, feed, requester)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestDeleteFollowRequestsByRequester(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	feed1 := newPrivateUserFeed(t, db, "feed-one")
	feed2 := newPrivateUserFeed(t, db, "feed-two")
	requester := newGroupUser(t, db, "requester")
	other := newGroupUser(t, db, "other")
	for _, feed := range []uuid.UUID{feed1, feed2} {
		require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
			return StageRequestFollow(db, batch, feed, requester, time.Now())
		}))
	}
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRequestFollow(db, batch, feed1, other, time.Now())
	}))

	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageDeleteFollowRequestsByRequester(db, batch, requester)
	}))

	for _, feed := range []uuid.UUID{feed1, feed2} {
		pending, err := IsFollowRequestPending(db, feed, requester)
		require.NoError(t, err)
		require.False(t, pending)
	}
	pending, err := IsFollowRequestPending(db, feed1, other)
	require.NoError(t, err)
	require.True(t, pending)
}
