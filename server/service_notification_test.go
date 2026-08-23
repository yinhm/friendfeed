package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
	"google.golang.org/protobuf/proto"
)

func readyServiceNotificationTask(t *testing.T, srv *ApiServer, deadAtMS int64) *pb.Task {
	t.Helper()
	records, err := taskqueue.List(srv.rdb, "ready", 100)
	require.NoError(t, err)
	for _, record := range records {
		if record.Task == nil || record.Task.Type != notificationFeedServiceFailedTaskType {
			continue
		}
		var payload feedServiceFailedNotificationPayload
		require.NoError(t, json.Unmarshal(record.Task.Payload, &payload))
		if deadAtMS == 0 || payload.DeadAtMS == deadAtMS {
			return record.Task
		}
	}
	t.Fatalf("ready task %q for dead_at_ms=%d not found", notificationFeedServiceFailedTaskType, deadAtMS)
	return nil
}

func serviceFailureNotifications(t *testing.T, srv *ApiServer, recipient uuid.UUID) []model.NotificationRecord {
	t.Helper()
	records, _, err := model.ListNotifications(srv.rdb, recipient, 100, nil)
	require.NoError(t, err)
	result := make([]model.NotificationRecord, 0, len(records))
	for _, record := range records {
		if record.Kind == model.NotificationFeedServiceFailed {
			result = append(result, record)
		}
	}
	return result
}

func TestFeedServiceDeadTransitionNotifiesPersonalFeedOnce(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	user := createServiceUser(t, srv, "failed-source-owner")
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind,
		Url: "https://gone.example/feed?token=must-not-leak",
	})
	require.NoError(t, err)
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		return &serviceFetchResult{status: http.StatusGone}, &serviceSourceError{
			permanent: true,
			err:       errors.New("Service HTTP status 410 with private response detail"),
		}
	}
	payload, err := proto.Marshal(&pb.FeedServiceSeedPayload{
		ServiceUuid: binding.ServiceUuid, TargetFeedUuid: user.String(), ServiceId: binding.Id,
	})
	require.NoError(t, err)

	for attempt := uint32(1); attempt <= servicePermanentLimit; attempt++ {
		require.NoError(t, srv.handleServiceTask(context.Background(), &pb.Task{
			Type: feedServiceSeedTaskType, Payload: payload, Attempts: 3, MaxAttempts: 3,
		}))
		state, err := model.GetServiceState(srv.rdb, serviceID)
		require.NoError(t, err)
		if attempt < servicePermanentLimit {
			require.Equal(t, model.ServiceStatusDegraded, state.Status)
			require.Empty(t, serviceFailureNotifications(t, srv, user))
			records, err := taskqueue.List(srv.rdb, "ready", 100)
			require.NoError(t, err)
			for _, record := range records {
				require.NotEqual(t, notificationFeedServiceFailedTaskType, record.Task.Type,
					"degraded source must not enqueue a failure notification")
			}
		}
		now = now.Add(2 * 24 * time.Hour)
	}

	state, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, model.ServiceStatusDead, state.Status)
	require.Positive(t, state.DeadAtMs)

	root := readyServiceNotificationTask(t, srv, state.DeadAtMs)
	require.NotContains(t, string(root.Payload), "gone.example")
	require.NotContains(t, string(root.Payload), "token")
	require.NotContains(t, string(root.Payload), "private response detail")

	require.NoError(t, srv.handleFeedServiceFailedNotificationTask(context.Background(), root))
	require.NoError(t, srv.handleFeedServiceFailedNotificationTask(context.Background(), root), "task retry must be idempotent")
	notifications := serviceFailureNotifications(t, srv, user)
	require.Len(t, notifications, 1)
	require.Equal(t, user.String(), notifications[0].RecipientUUID)
	require.Equal(t, user.String(), notifications[0].TargetUUID)
	require.Equal(t, state.DeadAtMs, notifications[0].ActivityAtMS)
}

func TestFeedServiceFailureNotifiesCurrentGroupAdmins(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	admin1 := createServiceUser(t, srv, "service-admin-1")
	admin2 := createServiceUser(t, srv, "service-admin-2")
	member := createServiceUser(t, srv, "service-member")
	group := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{
		Uuid: group.String(), Id: "service-group", Name: "Service Group", Type: "group",
	}))
	for _, user := range []uuid.UUID{admin1, admin2, member} {
		require.NoError(t, model.JoinGroup(srv.rdb, group, user))
	}
	require.NoError(t, model.AddGroupAdmin(srv.rdb, group, admin1))
	require.NoError(t, model.AddGroupAdmin(srv.rdb, group, admin2))

	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: admin1.String(), TargetFeedUuid: group.String(), Kind: model.WebFeedServiceKind,
		Url: "https://example.com/group-feed",
	})
	require.NoError(t, err)
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	deadAt := now.Add(time.Hour).UnixMilli()
	spec, err := feedServiceFailedNotificationSpec(serviceID, deadAt)
	require.NoError(t, err)
	root := &pb.Task{Type: spec.Type, Payload: spec.Payload, PayloadVersion: spec.PayloadVersion}

	require.NoError(t, srv.handleFeedServiceFailedNotificationTask(context.Background(), root))
	require.NoError(t, srv.handleFeedServiceFailedNotificationTask(context.Background(), root), "task retry must be idempotent")
	for _, admin := range []uuid.UUID{admin1, admin2} {
		notifications := serviceFailureNotifications(t, srv, admin)
		require.Len(t, notifications, 1)
		require.Equal(t, group.String(), notifications[0].TargetUUID)
	}
	require.Empty(t, serviceFailureNotifications(t, srv, member), "ordinary Group member must not be notified")
	require.Empty(t, serviceFailureNotifications(t, srv, group), "Group profile itself can never be a recipient")
}

func TestFeedServiceFailureDeduplicatesMultipleBindingsForSameTarget(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	user := createServiceUser(t, srv, "duplicate-binding-owner")
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind,
		Url: "https://example.com/shared-source",
	})
	require.NoError(t, err)
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	alias := proto.Clone(binding).(*pb.FeedService)
	alias.Id = "legacy-alias"
	alias.Enabled = true
	alias.Created = now.Unix()
	require.NoError(t, model.PutFeedService(srv.rdb, user, alias))
	aliasIndex, err := model.ServiceFeedIndexKey(serviceID, user, alias.Id)
	require.NoError(t, err)
	require.NoError(t, srv.rdb.Set(aliasIndex, nil))

	deadAt := now.Add(time.Hour).UnixMilli()
	spec, err := feedServiceFailedNotificationSpec(serviceID, deadAt)
	require.NoError(t, err)
	task := &pb.Task{Type: spec.Type, Payload: spec.Payload, PayloadVersion: spec.PayloadVersion}
	require.NoError(t, srv.handleFeedServiceFailedNotificationTask(context.Background(), task))
	require.NoError(t, srv.handleFeedServiceFailedNotificationTask(context.Background(), task))
	require.Len(t, serviceFailureNotifications(t, srv, user), 1)
}

func TestFeedServiceRecoveryStartsNewFailureOccurrence(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	user := createServiceUser(t, srv, "recovering-source-owner")
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind,
		Url: "https://example.com/recovering-source",
	})
	require.NoError(t, err)
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	firstDeadAt := now.Add(8 * 24 * time.Hour).UnixMilli()
	firstSpec, err := feedServiceFailedNotificationSpec(serviceID, firstDeadAt)
	require.NoError(t, err)
	require.NoError(t, srv.handleFeedServiceFailedNotificationTask(context.Background(), &pb.Task{
		Type: firstSpec.Type, Payload: firstSpec.Payload, PayloadVersion: firstSpec.PayloadVersion,
	}))
	require.Len(t, serviceFailureNotifications(t, srv, user), 1)

	deadState, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	deadState.Status = model.ServiceStatusDead
	deadState.DeadAtMs = firstDeadAt
	deadState.PermanentFailures = servicePermanentLimit
	deadState.PermanentFailureSinceMs = now.UnixMilli()
	deadState.NextFetchMs = 0
	require.NoError(t, model.PutServiceState(srv.rdb, serviceID, deadState))

	now = time.UnixMilli(firstDeadAt).Add(time.Hour).UTC()
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		return &serviceFetchResult{status: http.StatusOK, feed: &gofeed.Feed{}}, nil
	}
	seedPayload, err := proto.Marshal(&pb.FeedServiceSeedPayload{
		ServiceUuid: binding.ServiceUuid, TargetFeedUuid: user.String(), ServiceId: binding.Id,
	})
	require.NoError(t, err)
	require.NoError(t, srv.handleServiceTask(context.Background(), &pb.Task{
		Type: feedServiceSeedTaskType, Payload: seedPayload, Attempts: 1, MaxAttempts: 3,
	}))
	recovered, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, model.ServiceStatusActive, recovered.Status)
	require.Zero(t, recovered.DeadAtMs, "successful manual Refresh must close the old failure cycle")

	secondDeadAt := now.Add(8 * 24 * time.Hour).UTC()
	recovered.Status = model.ServiceStatusDegraded
	recovered.PermanentFailures = servicePermanentLimit - 1
	recovered.PermanentFailureSinceMs = secondDeadAt.Add(-8 * 24 * time.Hour).UnixMilli()
	recovered.NextFetchMs = 0
	require.NoError(t, model.PutServiceState(srv.rdb, serviceID, recovered))
	now = secondDeadAt
	srv.serviceFetch = func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error) {
		return &serviceFetchResult{status: http.StatusGone}, &serviceSourceError{permanent: true, err: errors.New("gone again")}
	}
	fetchPayload, err := proto.Marshal(&pb.ServiceFetchPayload{ServiceUuid: binding.ServiceUuid})
	require.NoError(t, err)
	require.NoError(t, srv.handleServiceTask(context.Background(), &pb.Task{
		Type: serviceFetchTaskType, Payload: fetchPayload, CreatedAtMs: now.UnixMilli(), Attempts: 3, MaxAttempts: 3,
	}))
	failedAgain, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, model.ServiceStatusDead, failedAgain.Status)
	require.Equal(t, secondDeadAt.UnixMilli(), failedAgain.DeadAtMs)

	secondRoot := readyServiceNotificationTask(t, srv, failedAgain.DeadAtMs)
	require.NoError(t, srv.handleFeedServiceFailedNotificationTask(context.Background(), secondRoot))
	notifications := serviceFailureNotifications(t, srv, user)
	require.Len(t, notifications, 2)
	require.NotEqual(t, notifications[0].ID, notifications[1].ID, "new dead cycle needs a new deterministic occurrence")
}
