package server

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	taskqueue "github.com/yinhm/friendfeed/task"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/protobuf/proto"
)

func newRSSServer(t *testing.T) *ApiServer {
	t.Helper()
	dbpath := t.TempDir()
	cfg, err := util.NewConfigFromJSON("../conf/example.config.json")
	require.NoError(t, err)
	search.InitMockIndexService(filepath.Join(dbpath, "index"))
	srv, err := NewApiServer(dbpath, cfg)
	require.NoError(t, err)
	t.Cleanup(srv.Shutdown)
	return srv
}

func createRSSSubscriber(t *testing.T, srv *ApiServer) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{Uuid: id.String(), Id: "subscriber-" + id.String()[:8], Name: "Subscriber", Type: "user"}))
	return id
}

func TestRSSSubscriptionRPCs(t *testing.T) {
	srv := newRSSServer(t)
	userID := createRSSSubscriber(t, srv)
	now := time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }

	first, err := srv.SubscribeService(context.Background(), &pb.SubscribeServiceRequest{UserUuid: userID.String(), Url: "HTTPS://Example.COM:443/feed?b=2&a=1#frag"})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/feed?a=1&b=2", first.Url)
	second, err := srv.SubscribeService(context.Background(), &pb.SubscribeServiceRequest{UserUuid: userID.String(), Url: first.Url})
	require.NoError(t, err)
	require.Equal(t, first.FeedUuid, second.FeedUuid)

	listed, err := srv.ListSubscriptions(context.Background(), &pb.ListSubscriptionsRequest{UserUuid: userID.String()})
	require.NoError(t, err)
	require.Len(t, listed.Subscriptions, 1)
	feedID := uuid.Must(uuid.FromString(first.FeedUuid))
	hasFollowers, err := model.SubscriptionHasFollowers(srv.rdb, feedID)
	require.NoError(t, err)
	require.True(t, hasFollowers)

	_, err = srv.UnsubscribeService(context.Background(), &pb.UnsubscribeServiceRequest{UserUuid: userID.String(), FeedUuid: first.FeedUuid})
	require.NoError(t, err)
	hasFollowers, err = model.SubscriptionHasFollowers(srv.rdb, feedID)
	require.NoError(t, err)
	require.False(t, hasFollowers)
}

func TestRSSHandlerImportsStableEntriesAndAdvancesState(t *testing.T) {
	srv := newRSSServer(t)
	userID := createRSSSubscriber(t, srv)
	now := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	subscription, err := model.SubscribeRSS(srv.rdb, userID, "https://example.com/feed", now)
	require.NoError(t, err)
	srv.rssFetch = func(context.Context, *pb.Subscription, *pb.SubscriptionState) (*rssFetchResult, error) {
		published := now.Add(-time.Minute)
		return &rssFetchResult{status: 200, etag: `"v1"`, feed: &gofeed.Feed{Items: []*gofeed.Item{{GUID: "item-1", Title: "Hello", Link: "https://example.com/1", Content: "<p>safe</p><script>bad()</script>", PublishedParsed: &published}}}}, nil
	}
	payload, err := proto.Marshal(&pb.RSSFetchPayload{FeedUuid: subscription.FeedUuid})
	require.NoError(t, err)
	task := &pb.Task{Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 1, MaxAttempts: 3}
	require.NoError(t, srv.handleRSSFetchTask(context.Background(), task))
	entryID := model.UniqueKeyFrom("rss", subscription.Url, "item-1")
	entry, err := model.GetEntry(srv.rdb, entryID.String())
	require.NoError(t, err)
	require.NotContains(t, entry.Body, "script")
	require.Equal(t, &pb.Via{Name: subscription.Title, Url: subscription.Url}, entry.Via)
	require.NoError(t, srv.handleRSSFetchTask(context.Background(), task))
	state, err := model.GetSubscriptionState(srv.rdb, uuid.Must(uuid.FromString(subscription.FeedUuid)))
	require.NoError(t, err)
	require.Equal(t, `"v1"`, state.Etag)
	require.Equal(t, now.Add(rssFetchInterval).UnixMilli(), state.NextFetchMs)
}

func TestRSSHandlerFinalFailureAdvancesState(t *testing.T) {
	srv := newRSSServer(t)
	userID := createRSSSubscriber(t, srv)
	now := time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	subscription, err := model.SubscribeRSS(srv.rdb, userID, "https://example.com/feed", now)
	require.NoError(t, err)
	srv.rssFetch = func(context.Context, *pb.Subscription, *pb.SubscriptionState) (*rssFetchResult, error) {
		return nil, errors.New("offline")
	}
	payload, err := proto.Marshal(&pb.RSSFetchPayload{FeedUuid: subscription.FeedUuid})
	require.NoError(t, err)
	err = srv.handleRSSFetchTask(context.Background(), &pb.Task{Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 3, MaxAttempts: 3})
	require.ErrorContains(t, err, "offline")
	state, err := model.GetSubscriptionState(srv.rdb, uuid.Must(uuid.FromString(subscription.FeedUuid)))
	require.NoError(t, err)
	require.EqualValues(t, 1, state.ConsecutiveFailures)
	require.Greater(t, state.NextFetchMs, now.UnixMilli())
}

func TestRSSPublicIPPolicy(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.1.1", "::1", "fc00::1", "0.0.0.0"} {
		require.False(t, rssPublicIP(netip.MustParseAddr(raw)), raw)
	}
	require.True(t, rssPublicIP(netip.MustParseAddr("8.8.8.8")))
}

func TestRSSHandlerTreatsDeletedSubscriptionAsNoop(t *testing.T) {
	srv := newRSSServer(t)
	userID := createRSSSubscriber(t, srv)
	now := time.Date(2026, 8, 14, 4, 0, 0, 0, time.UTC)
	subscription, err := model.SubscribeRSS(srv.rdb, userID, "https://example.com/deleted", now)
	require.NoError(t, err)
	feedID := uuid.Must(uuid.FromString(subscription.FeedUuid))
	require.NoError(t, model.Subscription.Delete(srv.rdb, feedID.Bytes()))
	payload, err := proto.Marshal(&pb.RSSFetchPayload{FeedUuid: subscription.FeedUuid})
	require.NoError(t, err)
	require.NoError(t, srv.handleRSSFetchTask(context.Background(), &pb.Task{Payload: payload, CreatedAtMs: now.UnixMilli()}))
}

func TestRSSSchedulerSkipsCorruptStateAndContinues(t *testing.T) {
	srv := newRSSServer(t)
	userID := createRSSSubscriber(t, srv)
	now := time.Date(2026, 8, 14, 5, 0, 0, 0, time.UTC)
	_, err := model.SubscribeRSS(srv.rdb, userID, "https://example.com/valid", now)
	require.NoError(t, err)
	require.NoError(t, srv.rdb.Set(model.SubscriptionState.PrefixAppend(make([]byte, uuid.Size)), []byte{0xff}))
	require.NoError(t, srv.scheduleDueRSS(context.Background(), now))
	records, err := taskqueue.List(srv.rdb, "ready", 10)
	require.NoError(t, err)
	require.Len(t, records, 1)
}
