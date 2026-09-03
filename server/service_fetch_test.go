package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/media"
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
	disabled, err := srv.SetFeedServiceEnabled(context.Background(), &pb.SetFeedServiceEnabledRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), ServiceId: first.Id, Enabled: false,
	})
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
	bindings, err = model.ListServiceFeedBindings(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Empty(t, bindings)
	_, err = srv.RefreshFeedService(context.Background(), &pb.RefreshFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), ServiceId: first.Id,
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	_, err = srv.SetFeedServiceEnabled(context.Background(), &pb.SetFeedServiceEnabledRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), ServiceId: first.Id, Enabled: true,
	})
	require.NoError(t, err)
	_, err = srv.RefreshFeedService(context.Background(), &pb.RefreshFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), ServiceId: first.Id,
	})
	require.NoError(t, err)
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
	require.NoError(t, model.JoinGroup(srv.rdb, group, admin))
	require.NoError(t, model.AddGroupAdmin(srv.rdb, group, admin))
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
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	deadState, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	deadState.Status = model.ServiceStatusDead
	deadState.PermanentFailures = servicePermanentLimit
	deadState.DeadAtMs = now.Add(-time.Hour).UnixMilli()
	require.NoError(t, model.PutServiceState(srv.rdb, serviceID, deadState))
	published := now.Add(-time.Hour)
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		return &serviceFetchResult{status: 200, etag: `"v1"`, feed: &gofeed.Feed{Title: "Example Feed", Items: []*gofeed.Item{{GUID: "item-1", Title: "Hello", Link: "https://example.com/1", Content: "<p>safe</p><script>bad()</script>", PublishedParsed: &published}}}}, nil
	}
	payload, err := proto.Marshal(&pb.FeedServiceSeedPayload{ServiceUuid: binding.ServiceUuid, TargetFeedUuid: user.String(), ServiceId: binding.Id})
	require.NoError(t, err)
	task := &pb.Task{Type: feedServiceSeedTaskType, Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 1, MaxAttempts: 3}
	require.NoError(t, srv.handleServiceTask(context.Background(), task))
	require.NoError(t, srv.handleServiceTask(context.Background(), task))
	service, err := model.GetService(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, "Example Feed", service.Title)
	binding, err = model.GetFeedService(srv.rdb, user, binding.Id)
	require.NoError(t, err)
	require.Equal(t, "Example Feed", binding.Name)
	entryID := model.UniqueKeyFrom("external-entry", user.String(), serviceID.String(), "item-1")
	entry, err := model.GetEntry(srv.rdb, entryID.String())
	require.NoError(t, err)
	require.Equal(t, user.String(), entry.ProfileUuid)
	require.Equal(t, user.String(), entry.FeedUuid)
	require.NotContains(t, entry.Body, "script")
	state, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, now.Add(serviceFetchInterval).UnixMilli(), state.NextFetchMs)
	require.Equal(t, model.ServiceStatusActive, state.Status)
	require.Equal(t, now.UnixMilli(), state.LastSuccessMs)
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

func TestServiceHandlerDisablesMissingTargetAndContinuesBindings(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	ids := []uuid.UUID{
		uuid.Must(uuid.FromString("00000000-0000-4000-8000-000000000001")),
		uuid.Must(uuid.FromString("00000000-0000-4000-8000-000000000002")),
		uuid.Must(uuid.FromString("00000000-0000-4000-8000-000000000003")),
	}
	for i, id := range ids {
		require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{
			Uuid: id.String(), Id: fmt.Sprintf("reader-%d", i), Name: fmt.Sprintf("Reader %d", i), Type: "user",
		}))
	}
	var bindings []*pb.FeedService
	for _, id := range ids {
		binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
			ActorUuid: id.String(), TargetFeedUuid: id.String(), Kind: model.WebFeedServiceKind,
			Url: "https://example.com/shared-feed",
		})
		require.NoError(t, err)
		bindings = append(bindings, binding)
	}
	deleted, err := model.GetProfileFromUuid(srv.rdb, ids[1])
	require.NoError(t, err)
	deleted.Deleted = true
	require.NoError(t, model.UpdateProfile(srv.rdb, deleted))

	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		return &serviceFetchResult{status: 200, feed: &gofeed.Feed{
			Title: "Shared Feed", Items: []*gofeed.Item{{GUID: "shared-item", Title: "Hello"}},
		}}, nil
	}
	serviceID := uuid.Must(uuid.FromString(bindings[0].ServiceUuid))
	payload, err := proto.Marshal(&pb.ServiceFetchPayload{ServiceUuid: serviceID.String()})
	require.NoError(t, err)
	require.NoError(t, srv.handleServiceTask(context.Background(), &pb.Task{
		Type: serviceFetchTaskType, Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 1, MaxAttempts: 3,
	}))

	for _, target := range []uuid.UUID{ids[0], ids[2]} {
		entryID := model.UniqueKeyFrom("external-entry", target.String(), serviceID.String(), "shared-item")
		_, err := model.GetEntry(srv.rdb, entryID.String())
		require.NoError(t, err)
	}
	deletedEntryID := model.UniqueKeyFrom("external-entry", ids[1].String(), serviceID.String(), "shared-item")
	_, err = model.GetEntry(srv.rdb, deletedEntryID.String())
	require.ErrorIs(t, err, model.ErrNotFound)
	stale, err := model.GetFeedService(srv.rdb, ids[1], bindings[1].Id)
	require.NoError(t, err)
	require.False(t, stale.Enabled)
	indexKey, err := model.ServiceFeedIndexKey(serviceID, ids[1], bindings[1].Id)
	require.NoError(t, err)
	indexed, err := srv.rdb.Exists(indexKey)
	require.NoError(t, err)
	require.False(t, indexed)
	state, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Greater(t, state.NextFetchMs, now.UnixMilli())
}

func TestServiceHandlerAttemptsLaterBindingsBeforeReturningErrors(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 16, 11, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	brokenTarget := uuid.Must(uuid.FromString("00000000-0000-4000-8000-000000000010"))
	healthyTarget := uuid.Must(uuid.FromString("00000000-0000-4000-8000-000000000020"))
	for i, id := range []uuid.UUID{brokenTarget, healthyTarget} {
		require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{
			Uuid: id.String(), Id: fmt.Sprintf("target-%d", i), Name: fmt.Sprintf("Target %d", i), Type: "user",
		}))
	}
	var bindings []*pb.FeedService
	for _, id := range []uuid.UUID{brokenTarget, healthyTarget} {
		binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
			ActorUuid: id.String(), TargetFeedUuid: id.String(), Kind: model.WebFeedServiceKind,
			Url: "https://example.com/error-isolation",
		})
		require.NoError(t, err)
		bindings = append(bindings, binding)
	}
	brokenKey, err := model.FeedServiceKey(brokenTarget, bindings[0].Id)
	require.NoError(t, err)
	require.NoError(t, srv.rdb.Put(brokenKey, []byte{0xff}))
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		return &serviceFetchResult{status: 200, feed: &gofeed.Feed{
			Title: "Isolation Feed", Items: []*gofeed.Item{{GUID: "isolation-item", Title: "Hello"}},
		}}, nil
	}
	serviceID := uuid.Must(uuid.FromString(bindings[0].ServiceUuid))
	payload, err := proto.Marshal(&pb.ServiceFetchPayload{ServiceUuid: serviceID.String()})
	require.NoError(t, err)
	initialState, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	err = srv.handleServiceTask(context.Background(), &pb.Task{
		Type: serviceFetchTaskType, Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 1, MaxAttempts: 3,
	})
	require.ErrorContains(t, err, "load FeedService")
	midState, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, initialState.NextFetchMs, midState.NextFetchMs)
	require.Zero(t, midState.DeliveryFailures, "an attempt that can still retry must not advance business cooldown")
	err = srv.handleServiceTask(context.Background(), &pb.Task{
		Type: serviceFetchTaskType, Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 3, MaxAttempts: 3,
	})
	require.NoError(t, err, "persisted delivery cooldown resolves the exhausted Task")
	healthyEntryID := model.UniqueKeyFrom("external-entry", healthyTarget.String(), serviceID.String(), "isolation-item")
	_, getErr := model.GetEntry(srv.rdb, healthyEntryID.String())
	require.NoError(t, getErr)
	state, stateErr := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, stateErr)
	require.Equal(t, now.Add(time.Hour).UnixMilli(), state.NextFetchMs, "delivery errors must enter a bounded cooldown")
	require.Equal(t, model.ServiceStatusActive, state.Status)
	require.Equal(t, uint32(1), state.DeliveryFailures)
	require.Empty(t, state.LastError, "binding details must not leak through source status")
}

func TestPermanentServiceFailuresBecomeDead(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	err404 := &serviceSourceError{err: errors.New("Service HTTP status 404"), permanent: true}
	shortOutage := &pb.ServiceState{ServiceUuid: uuid.Must(uuid.NewV4()).String()}
	for attempt := range servicePermanentLimit {
		applyServiceFetchFailure(shortOutage, &serviceFetchResult{status: http.StatusNotFound}, err404, now.Add(time.Duration(attempt)*time.Hour))
	}
	require.Equal(t, model.ServiceStatusDegraded, shortOutage.Status, "failure count alone must not kill a source")
	applyServiceFetchFailure(shortOutage, nil, errors.New("temporary network failure"), now.Add(7*time.Hour))
	require.Zero(t, shortOutage.PermanentFailures)
	require.Zero(t, shortOutage.PermanentFailureSinceMs, "a transient failure breaks the permanent-candidate sequence")

	state := &pb.ServiceState{ServiceUuid: uuid.Must(uuid.NewV4()).String()}
	for attempt := uint32(1); attempt <= servicePermanentLimit; attempt++ {
		attemptTime := now.Add(time.Duration(attempt-1) * 2 * 24 * time.Hour)
		applyServiceFetchFailure(state, &serviceFetchResult{status: http.StatusNotFound}, err404, attemptTime)
		if attempt < servicePermanentLimit {
			require.Equal(t, model.ServiceStatusDegraded, state.Status)
			require.Greater(t, state.NextFetchMs, attemptTime.UnixMilli())
		}
	}
	require.Equal(t, model.ServiceStatusDead, state.Status)
	require.Equal(t, now.Add(10*24*time.Hour).UnixMilli(), state.DeadAtMs)
	require.Zero(t, state.NextFetchMs)
	require.Equal(t, now.UnixMilli(), state.PermanentFailureSinceMs)
}

func TestExhaustedSourceFailureCompletesTaskAndPersistsDeadLifecycle(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 16, 12, 30, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	user := createServiceUser(t, srv, "gone-reader")
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind, Url: "https://gone.example/feed",
	})
	require.NoError(t, err)
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		result := &serviceFetchResult{status: http.StatusGone}
		return result, &serviceSourceError{err: errors.New("Service HTTP status 410"), permanent: true}
	}
	payload, err := proto.Marshal(&pb.FeedServiceSeedPayload{ServiceUuid: binding.ServiceUuid, TargetFeedUuid: user.String(), ServiceId: binding.Id})
	require.NoError(t, err)
	for range servicePermanentLimit {
		err = srv.handleServiceTask(context.Background(), &pb.Task{Type: feedServiceSeedTaskType, Payload: payload, Attempts: 3, MaxAttempts: 3})
		require.NoError(t, err, "a persisted lifecycle outcome completes the exhausted Task")
		now = now.Add(2 * 24 * time.Hour)
	}
	state, err := model.GetServiceState(srv.rdb, uuid.Must(uuid.FromString(binding.ServiceUuid)))
	require.NoError(t, err)
	require.Equal(t, model.ServiceStatusDead, state.Status)
	require.Equal(t, uint32(servicePermanentLimit), state.PermanentFailures)
}

func TestFailedSeedProbeOnDeadSourceReentersScheduling(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 16, 12, 30, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	user := createServiceUser(t, srv, "dead-reader")
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind, Url: "https://dead.example/feed",
	})
	require.NoError(t, err)
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	deadState, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	deadState.Status = model.ServiceStatusDead
	deadState.PermanentFailures = servicePermanentLimit
	deadState.PermanentFailureSinceMs = now.Add(-10 * 24 * time.Hour).UnixMilli()
	deadState.DeadAtMs = now.Add(-time.Hour).UnixMilli()
	deadState.NextFetchMs = 0
	require.NoError(t, model.PutServiceState(srv.rdb, serviceID, deadState))
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		return &serviceFetchResult{status: http.StatusServiceUnavailable}, errors.New("temporary network failure")
	}
	payload, err := proto.Marshal(&pb.FeedServiceSeedPayload{ServiceUuid: binding.ServiceUuid, TargetFeedUuid: user.String(), ServiceId: binding.Id})
	require.NoError(t, err)
	task := &pb.Task{Type: feedServiceSeedTaskType, Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 3, MaxAttempts: 3}
	require.NoError(t, srv.handleServiceTask(context.Background(), task), "a resolved lifecycle outcome completes the exhausted seed Task")
	state, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, model.ServiceStatusDegraded, state.Status, "a failed manual probe must re-enter scheduling, not stay frozen dead")
	require.Zero(t, state.DeadAtMs)
	require.Greater(t, state.NextFetchMs, now.UnixMilli(), "the source must get a real next-fetch time instead of 0")
}

func TestFailedSeedProbeWithPermanentErrorReentersScheduling(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 16, 12, 30, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	user := createServiceUser(t, srv, "dead-reader-perm")
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind, Url: "https://dead-perm.example/feed",
	})
	require.NoError(t, err)
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))

	// Set up a dead state: PermanentFailures >= 6, window expired, NextFetchMs=0
	deadState, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	deadState.Status = model.ServiceStatusDead
	deadState.PermanentFailures = servicePermanentLimit
	deadState.PermanentFailureSinceMs = now.Add(-10 * 24 * time.Hour).UnixMilli()
	deadState.DeadAtMs = now.Add(-time.Hour).UnixMilli()
	deadState.NextFetchMs = 0
	require.NoError(t, model.PutServiceState(srv.rdb, serviceID, deadState))

	// Mock a permanent error (410 Gone)
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		return &serviceFetchResult{status: http.StatusGone}, &serviceSourceError{permanent: true, err: errors.New("feed gone")}
	}

	payload, err := proto.Marshal(&pb.FeedServiceSeedPayload{ServiceUuid: binding.ServiceUuid, TargetFeedUuid: user.String(), ServiceId: binding.Id})
	require.NoError(t, err)
	task := &pb.Task{Type: feedServiceSeedTaskType, Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 3, MaxAttempts: 3}
	require.NoError(t, srv.handleServiceTask(context.Background(), task), "exhausted seed task must complete even with permanent error")

	state, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)

	// The key fix: permanent error resets the candidate window (PF=1, degraded, 1h backoff)
	// instead of immediately returning to Dead with NextFetchMs=0
	require.Equal(t, model.ServiceStatusDegraded, state.Status, "permanent error on dead source should enter degraded, not stay dead")
	require.Equal(t, uint32(1), state.PermanentFailures, "candidate window should be reset to PF=1")
	require.Zero(t, state.DeadAtMs, "DeadAtMs should be cleared")
	require.Greater(t, state.NextFetchMs, now.UnixMilli(), "must schedule next fetch (1h backoff), not freeze at 0")

	// Verify it's actually scheduled for ~1 hour later (degraded + PF=1 backoff)
	nextFetch := time.UnixMilli(state.NextFetchMs)
	expectedBackoff := now.Add(time.Hour)
	require.WithinDuration(t, expectedBackoff, nextFetch, 5*time.Minute, "should have ~1h backoff")
}

func TestServiceHandlerPersistsPermanentRedirectWithoutChangingIdentity(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 16, 13, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	user := createServiceUser(t, srv, "moved-reader")
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind,
		Url: "https://old.example/feed",
	})
	require.NoError(t, err)
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		return &serviceFetchResult{status: http.StatusOK, feed: &gofeed.Feed{}, permanentRedirect: true, finalURL: "https://new.example/feed"}, nil
	}
	payload, err := proto.Marshal(&pb.ServiceFetchPayload{ServiceUuid: binding.ServiceUuid})
	require.NoError(t, err)
	require.NoError(t, srv.handleServiceTask(context.Background(), &pb.Task{Type: serviceFetchTaskType, Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 1, MaxAttempts: 3}))
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	service, err := model.GetService(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, "https://old.example/feed", service.CanonicalUrl)
	require.Equal(t, "https://new.example/feed", service.FetchUrl)
	require.Equal(t, serviceID.String(), service.Uuid)
}

func TestSchedulerSkipsDeadService(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 16, 14, 0, 0, 0, time.UTC)
	user := createServiceUser(t, srv, "dead-reader")
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind, Url: "https://dead.example/feed"})
	require.NoError(t, err)
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	state, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	state.Status = model.ServiceStatusDead
	state.NextFetchMs = 0
	require.NoError(t, model.PutServiceState(srv.rdb, serviceID, state))
	fetched := false
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		fetched = true
		return &serviceFetchResult{status: http.StatusOK}, nil
	}
	payload, err := proto.Marshal(&pb.ServiceFetchPayload{ServiceUuid: serviceID.String()})
	require.NoError(t, err)
	require.NoError(t, srv.handleServiceTask(context.Background(), &pb.Task{
		Type: serviceFetchTaskType, Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 1, MaxAttempts: 3,
	}))
	require.False(t, fetched, "a stale scheduled task cannot revive a dead source")
	require.NoError(t, srv.scheduleDueServices(context.Background(), now))
	records, err := taskqueue.List(srv.rdb, "ready", 10)
	require.NoError(t, err)
	for _, record := range records {
		require.NotEqual(t, serviceFetchTaskType, record.Task.Type)
	}
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

type bingTestStorage struct{}

func (bingTestStorage) Exists(string) (bool, error) { return false, nil }
func (bingTestStorage) Fetch(object *media.Object) (*http.Response, error) {
	object.Content = []byte("\x89PNG\r\n\x1a\n")
	return nil, nil
}
func (bingTestStorage) Post(object *media.Object) (*media.Object, error) {
	object.Path = "a/b/original"
	return object, nil
}
func (bingTestStorage) Mirror(object *media.Object) (*media.Object, error) { return object, nil }
func (bingTestStorage) FromUrl(_ string, source string, _ string) (*media.Object, error) {
	return &media.Object{Url: source}, nil
}
func (bingTestStorage) Thumbnail(*media.Object) (*media.Object, error) {
	return &media.Object{Path: "a/b/original-1024.jpg", Width: 1024, Height: 576}, nil
}

func TestParseBingWallpaper(t *testing.T) {
	feed, err := parseBingWallpaper([]byte(`{"images":[{"fullstartdate":"202609030700","urlbase":"/th?id=OHR.Sample_EN-US123","copyright":"Sample © Author"}]}`))
	require.NoError(t, err)
	require.Equal(t, "Bing Wallpaper", feed.Title)
	require.Len(t, feed.Items, 1)
	require.Equal(t, "/th?id=OHR.Sample_EN-US123", feed.Items[0].GUID)
	require.Equal(t, "Sample © Author", feed.Items[0].Description)
	require.Equal(t, "https://www.bing.com/th?id=OHR.Sample_EN-US123_UHD.jpg", feed.Items[0].Link)
	require.Equal(t, "https://www.bing.com/th?id=OHR.Sample_EN-US123_UHD.jpg", feed.Items[0].Enclosures[0].URL)
	require.Equal(t, time.Date(2026, 9, 2, 23, 0, 0, 0, time.UTC), *feed.Items[0].PublishedParsed)

	_, err = parseBingWallpaper([]byte(`{"images":[{"fullstartdate":"202609030700","urlbase":"https://evil.example/x","copyright":"bad"}]}`))
	require.Error(t, err)
	require.False(t, validBingWallpaperURLBase("\x7f"))
}

func TestBingWallpaperServiceImportsMediaAndUsesDailyInterval(t *testing.T) {
	srv := newServiceServer(t)
	user := createServiceUser(t, srv, "wallpapers")
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		TargetFeedUuid: user.String(), Kind: model.BingWallpaperServiceKind,
	})
	require.NoError(t, err)
	require.Empty(t, binding.AddedByUuid)
	srv.fs = bingTestStorage{}
	published := now.Add(-time.Hour)
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		item := &gofeed.Item{
			GUID: "/th?id=OHR.Sample_EN-US123", Title: "Sample", Description: "Sample © Author", Link: "https://www.bing.com/th?id=OHR.Sample_EN-US123_UHD.jpg", PublishedParsed: &published,
			Enclosures: []*gofeed.Enclosure{{URL: "https://www.bing.com/th?id=OHR.Sample_EN-US123_UHD.jpg", Type: "image/jpeg"}},
		}
		return &serviceFetchResult{status: http.StatusOK, feed: &gofeed.Feed{Title: "Bing Wallpaper", Items: []*gofeed.Item{item}}}, nil
	}
	payload, err := proto.Marshal(&pb.FeedServiceSeedPayload{ServiceUuid: binding.ServiceUuid, TargetFeedUuid: user.String(), ServiceId: binding.Id})
	require.NoError(t, err)
	require.NoError(t, srv.handleServiceTask(context.Background(), &pb.Task{
		Type: feedServiceSeedTaskType, Payload: payload, CreatedAtMs: now.UnixMilli(), Attempts: 1, MaxAttempts: 3,
	}))
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	entryID := model.UniqueKeyFrom("external-entry", user.String(), serviceID.String(), "/th?id=OHR.Sample_EN-US123")
	entry, err := model.GetEntry(srv.rdb, entryID.String())
	require.NoError(t, err)
	require.Empty(t, entry.Files)
	require.Equal(t, "Sample © Author", entry.Body)
	require.Equal(t, srv.mediaBaseURL+"/a/b/original", entry.Url)
	require.Equal(t, srv.mediaBaseURL+"/a/b/original", entry.RawLink)
	require.Equal(t, srv.mediaBaseURL+"/a/b/original-1024.jpg", entry.Thumbnails[0].Url)
	require.Equal(t, srv.mediaBaseURL+"/a/b/original", entry.Thumbnails[0].Link)
	require.Equal(t, "https://www.bing.com/th?id=OHR.Sample_EN-US123_UHD.jpg", entry.Via.Url)
	state, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, now.Add(24*time.Hour).UnixMilli(), state.NextFetchMs)
}
