package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestNormalizeSubscriptionURL(t *testing.T) {
	got, err := NormalizeSubscriptionURL(" HTTPS://Example.COM:443/feed?b=2&a=1#fragment ")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/feed?a=1&b=2", got)
	_, err = NormalizeSubscriptionURL("file:///tmp/feed")
	require.Error(t, err)
	_, err = NormalizeSubscriptionURL("https://user:secret@example.com/feed")
	require.Error(t, err)
}

func TestSubscribeRSSCreatesSourceAndAtomicGraph(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	user := uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{Uuid: user.String(), Id: "reader", Type: "user"}))
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)

	created, err := SubscribeRSS(db, user, "https://example.com/feed", now)
	require.NoError(t, err)
	feedID := uuid.Must(uuid.FromString(created.FeedUuid))
	require.Equal(t, "https://example.com/feed", created.Url)
	profile, err := GetProfileFromUuid(db, feedID)
	require.NoError(t, err)
	require.Equal(t, "feed", profile.Type)
	state, err := GetSubscriptionState(db, feedID)
	require.NoError(t, err)
	require.Equal(t, now.UnixMilli(), state.NextFetchMs)
	follow, err := db.Exists(NewKeyFrom(Follow.Prefix, user.Bytes(), feedID.Bytes()))
	require.NoError(t, err)
	follower, err := db.Exists(NewKeyFrom(Follower.Prefix, feedID.Bytes(), user.Bytes()))
	require.NoError(t, err)
	require.True(t, follow && follower)

	again, err := SubscribeRSS(db, user, "HTTPS://EXAMPLE.COM:443/feed#x", now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, created.FeedUuid, again.FeedUuid)
	require.Equal(t, created.CreatedAtMs, again.CreatedAtMs)
	listed, err := ListRSSSubscriptions(db, user)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	hasFollowers, err := SubscriptionHasFollowers(db, feedID)
	require.NoError(t, err)
	require.True(t, hasFollowers)

	require.NoError(t, UnsubscribeRSS(db, user, feedID))
	hasFollowers, err = SubscriptionHasFollowers(db, feedID)
	require.NoError(t, err)
	require.False(t, hasFollowers)
}

func TestListRSSSubscriptionsSkipsOrdinaryFollows(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	user := uuid.Must(uuid.NewV4())
	ordinary := uuid.Must(uuid.NewV4())
	require.NoError(t, db.Set(NewKeyFrom(Follow.Prefix, user.Bytes(), ordinary.Bytes()), []byte("1")))
	listed, err := ListRSSSubscriptions(db, user)
	require.NoError(t, err)
	require.Empty(t, listed)
}

func TestSubscriptionRejectsZeroUUIDs(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	_, err = SubscribeRSS(db, uuid.Nil, "https://example.com/feed", time.Now())
	require.Error(t, err)
	require.Error(t, UnsubscribeRSS(db, uuid.Nil, uuid.Must(uuid.NewV4())))
	_, err = ListRSSSubscriptions(db, uuid.Nil)
	require.Error(t, err)
	require.Error(t, PutSubscriptionState(db, uuid.Nil, &pb.SubscriptionState{}))
}
