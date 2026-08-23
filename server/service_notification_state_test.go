package server

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func TestFeedServiceFailureKeepsCountersForAdminOfMultipleTargets(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	admin := createServiceUser(t, srv, "multi-group-service-admin")

	groups := make([]uuid.UUID, 2)
	for i := range groups {
		groups[i] = uuid.Must(uuid.NewV4())
		require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{
			Uuid: groups[i].String(), Id: "multi-service-group-" + string(rune('a'+i)),
			Name: "Multi Service Group", Type: "group",
		}))
		require.NoError(t, model.JoinGroup(srv.rdb, groups[i], admin))
		require.NoError(t, model.AddGroupAdmin(srv.rdb, groups[i], admin))
	}

	const sourceURL = "https://example.com/shared-multi-group-source"
	first, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: admin.String(), TargetFeedUuid: groups[0].String(), Kind: model.WebFeedServiceKind, Url: sourceURL,
	})
	require.NoError(t, err)
	second, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: admin.String(), TargetFeedUuid: groups[1].String(), Kind: model.WebFeedServiceKind, Url: sourceURL,
	})
	require.NoError(t, err)
	require.Equal(t, first.ServiceUuid, second.ServiceUuid, "same source URL must share the canonical Service")

	serviceID := uuid.Must(uuid.FromString(first.ServiceUuid))
	deadAt := now.Add(time.Hour).UnixMilli()
	spec, err := feedServiceFailedNotificationSpec(serviceID, deadAt)
	require.NoError(t, err)
	require.NoError(t, srv.handleFeedServiceFailedNotificationTask(context.Background(), &pb.Task{
		Type: spec.Type, Payload: spec.Payload, PayloadVersion: spec.PayloadVersion,
	}))

	notifications := serviceFailureNotifications(t, srv, admin)
	require.Len(t, notifications, 2)
	targets := map[string]bool{}
	for _, notification := range notifications {
		targets[notification.TargetUUID] = true
	}
	require.True(t, targets[groups[0].String()])
	require.True(t, targets[groups[1].String()])

	state, err := model.GetNotificationState(srv.rdb, admin)
	require.NoError(t, err)
	require.Equal(t, uint32(2), state.TotalCount)
	require.Equal(t, uint32(2), state.UnreadCount)
}
