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

func TestGroupFollowRequestNotificationFanoutIsBoundedAndRetryIdempotent(t *testing.T) {
	srv := newServiceServer(t)
	require.NoError(t, srv.ensureNotificationTaskDefinition())
	requester := createServiceUser(t, srv, "requester")
	group := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{
		Uuid: group.String(), Id: "group", Name: "Group", Type: "group", Private: true,
	}))

	admins := make([]uuid.UUID, 0, notificationFanoutPageSize+1)
	for i := 0; i < notificationFanoutPageSize+1; i++ {
		admin := createServiceUser(t, srv, "admin-"+uuid.Must(uuid.NewV4()).String())
		key, err := model.GroupAdminKey(group, admin)
		require.NoError(t, err)
		require.NoError(t, srv.rdb.Put(key, nil))
		admins = append(admins, admin)
	}

	requestedAt := time.Now().UTC().Format(time.RFC3339Nano)
	spec, err := groupFollowRequestNotificationSpec(group, requester, requestedAt, time.Now().UTC(), "")
	require.NoError(t, err)
	task := &pb.Task{Payload: spec.Payload}
	require.NoError(t, srv.handleGroupFollowRequestNotificationTask(context.Background(), task))
	// A retry of the same page must not duplicate recipient rows or counters.
	require.NoError(t, srv.handleGroupFollowRequestNotificationTask(context.Background(), task))

	notified := 0
	for _, admin := range admins {
		state, err := model.GetNotificationState(srv.rdb, admin)
		require.NoError(t, err)
		require.LessOrEqual(t, state.TotalCount, uint32(1))
		notified += int(state.TotalCount)
	}
	require.Equal(t, notificationFanoutPageSize, notified, "one task execution must not exceed its bounded page")
}
