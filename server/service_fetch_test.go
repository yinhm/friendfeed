package server

import (
	"context"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func newServiceServer(t *testing.T) *ApiServer {
	t.Helper()
	dbpath := t.TempDir()
	search.InitMockIndexService(filepath.Join(dbpath, "index"))
	srv, err := NewApiServer(dbpath, &util.Config{MediaPath: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(srv.Shutdown)
	return srv
}

func createServiceUser(t *testing.T, srv *ApiServer, id string) uuid.UUID {
	t.Helper()
	user := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{Uuid: user.String(), Id: id, Type: "user"}))
	return user
}

func TestFeedServiceRPCsDoNotCreateSocialGraph(t *testing.T) {
	srv := newServiceServer(t)
	user := createServiceUser(t, srv, "reader")
	srv.rssNow = func() time.Time { return time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC) }

	first, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind,
		Url: "HTTPS://Example.COM:443/feed?b=2&a=1#frag",
	})
	require.NoError(t, err)
	require.Equal(t, model.WebFeedServiceKind, first.Kind)
	require.True(t, first.Enabled)
	serviceID := uuid.Must(uuid.FromString(first.ServiceUuid))
	service, err := model.GetService(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/feed?b=2&a=1", service.CanonicalUrl)
	bindings, err := model.ListServiceFeedBindings(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	follows, err := srv.rdb.ForwardScan(model.NewKeyFrom(model.Follow.Prefix, user.Bytes()), func(int, []byte, []byte) error { return nil })
	require.NoError(t, err)
	require.Zero(t, follows)

	listed, err := srv.ListFeedServices(context.Background(), &pb.ListFeedServicesRequest{ActorUuid: user.String(), TargetFeedUuid: user.String()})
	require.NoError(t, err)
	require.Len(t, listed.Services, 1)
	_, err = srv.RemoveFeedService(context.Background(), &pb.RemoveFeedServiceRequest{ActorUuid: user.String(), TargetFeedUuid: user.String(), ServiceId: first.Id})
	require.NoError(t, err)
	bindings, err = model.ListServiceFeedBindings(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Empty(t, bindings)
}

func TestGroupFeedServiceRequiresExplicitAdmin(t *testing.T) {
	srv := newServiceServer(t)
	admin := createServiceUser(t, srv, "admin")
	member := createServiceUser(t, srv, "member")
	group := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{Uuid: group.String(), Id: "group", Type: "group"}))
	require.NoError(t, model.PutFeedinfo(srv.rdb, group.String(), &pb.Feedinfo{Uuid: group.String(), Id: "group", Type: "group", Admins: []*pb.Profile{{Uuid: admin.String(), Id: "admin"}}}))
	require.NoError(t, srv.rdb.Set(model.NewKeyFrom(model.Follow.Prefix, member.Bytes(), group.Bytes()), []byte("1")))

	request := &pb.AddFeedServiceRequest{ActorUuid: member.String(), TargetFeedUuid: group.String(), Kind: model.WebFeedServiceKind, Url: "https://example.com/feed"}
	_, err := srv.AddFeedService(context.Background(), request)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	request.ActorUuid = admin.String()
	_, err = srv.AddFeedService(context.Background(), request)
	require.NoError(t, err)
}

func TestServiceHandlerImportsStableEntriesAndAdvancesState(t *testing.T) {
	srv := newServiceServer(t)
	user := createServiceUser(t, srv, "reader")
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind, Url: "https://example.com/feed"})
	require.NoError(t, err)
	published := now.Add(-time.Hour)
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		return &serviceFetchResult{status: 200, etag: `"v1"`, feed: &gofeed.Feed{Items: []*gofeed.Item{{GUID: "item-1", Title: "Hello", Link: "https://example.com/1", Content: "<p>safe</p><script>bad()</script>", PublishedParsed: &published}}}}, nil
	}
	payload, err := proto.Marshal(&pb.FeedServiceSeedPayload{ServiceUuid: binding.ServiceUuid, TargetFeedUuid: user.String(), ServiceId: binding.Id})
	require.NoError(t, err)
	task := &pb.Task{Type: feedServiceSeedTaskType, Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 1, MaxAttempts: 3}
	require.NoError(t, srv.handleServiceTask(context.Background(), task))
	require.NoError(t, srv.handleServiceTask(context.Background(), task))
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	entryID := model.UniqueKeyFrom("external-entry", user.String(), serviceID.String(), "item-1")
	entry, err := model.GetEntry(srv.rdb, entryID.String())
	require.NoError(t, err)
	require.Equal(t, user.String(), entry.ProfileUuid)
	require.Equal(t, user.String(), entry.FeedUuid)
	require.NotContains(t, entry.Body, "script")
	state, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, now.Add(serviceFetchInterval).UnixMilli(), state.NextFetchMs)
}

func TestServiceHandlerTreatsDeletedBindingAsNoop(t *testing.T) {
	srv := newServiceServer(t)
	user := createServiceUser(t, srv, "reader")
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind, Url: "https://example.com/deleted"})
	require.NoError(t, err)
	_, err = srv.RemoveFeedService(context.Background(), &pb.RemoveFeedServiceRequest{ActorUuid: user.String(), TargetFeedUuid: user.String(), ServiceId: binding.Id})
	require.NoError(t, err)
	payload, err := proto.Marshal(&pb.FeedServiceSeedPayload{ServiceUuid: binding.ServiceUuid, TargetFeedUuid: user.String(), ServiceId: binding.Id})
	require.NoError(t, err)
	require.NoError(t, srv.handleServiceTask(context.Background(), &pb.Task{Type: feedServiceSeedTaskType, Payload: payload}))
}

func TestServiceSchedulerSkipsCorruptStateAndContinues(t *testing.T) {
	srv := newServiceServer(t)
	user := createServiceUser(t, srv, "reader")
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	_, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind, Url: "https://example.com/valid"})
	require.NoError(t, err)
	require.NoError(t, srv.rdb.Set(model.ServiceState.PrefixAppend(make([]byte, uuid.Size)), []byte{0xff}))
	require.NoError(t, srv.scheduleDueServices(context.Background(), now))
	records, err := taskqueue.List(srv.rdb, "ready", 10)
	require.NoError(t, err)
	require.NotEmpty(t, records)
}

func TestRSSPublicIPPolicy(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "100.64.0.1", "::1", "fc00::1"} {
		require.False(t, rssPublicIP(netip.MustParseAddr(raw)), raw)
	}
	require.True(t, rssPublicIP(netip.MustParseAddr("8.8.8.8")))
}

func TestRSSUserAgentIsCommonCompatibleForm(t *testing.T) {
	require.Contains(t, rssUserAgent, "Mozilla/5.0")
	require.Contains(t, rssUserAgent, "FriendFeed/1.0")
}
