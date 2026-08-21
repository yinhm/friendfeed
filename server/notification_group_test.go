package server

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func groupMembershipRequest(actor, group, target uuid.UUID) *pb.GroupMembershipRequest {
	return &pb.GroupMembershipRequest{
		ActorUuid:  actor.String(),
		GroupUuid:  group.String(),
		TargetUuid: target.String(),
	}
}

func TestGroupRoleNotificationsOnlyOnTransitions(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "notification-admin")
	member := createServiceUser(t, srv, "notification-member")
	group := createTestGroup(t, srv, creator, "notification-group")

	_, err := srv.JoinGroup(context.Background(), groupMembershipRequest(member, group, member))
	require.NoError(t, err)

	_, err = srv.AddGroupAdmin(context.Background(), groupMembershipRequest(creator, group, member))
	require.NoError(t, err)
	_, err = srv.AddGroupAdmin(context.Background(), groupMembershipRequest(creator, group, member))
	require.NoError(t, err)

	state, err := model.GetNotificationState(srv.rdb, member)
	require.NoError(t, err)
	require.Equal(t, uint32(1), state.TotalCount, "idempotent promote must not emit twice")

	_, err = srv.RemoveGroupAdmin(context.Background(), groupMembershipRequest(creator, group, member))
	require.NoError(t, err)
	_, err = srv.RemoveGroupAdmin(context.Background(), groupMembershipRequest(creator, group, member))
	require.NoError(t, err)

	state, err = model.GetNotificationState(srv.rdb, member)
	require.NoError(t, err)
	require.Equal(t, uint32(2), state.TotalCount, "idempotent demote must not emit twice")

	_, err = srv.RemoveGroupMember(context.Background(), groupMembershipRequest(creator, group, member))
	require.NoError(t, err)
	_, err = srv.RemoveGroupMember(context.Background(), groupMembershipRequest(creator, group, member))
	require.NoError(t, err)

	state, err = model.GetNotificationState(srv.rdb, member)
	require.NoError(t, err)
	require.Equal(t, uint32(3), state.TotalCount, "idempotent member removal must not emit twice")

	records, _, err := model.ListNotifications(srv.rdb, member, 10, nil)
	require.NoError(t, err)
	require.Len(t, records, 3)
	kinds := map[model.NotificationKind]bool{}
	for _, record := range records {
		kinds[record.Kind] = true
		require.Equal(t, creator.String(), record.ActorUUID)
		require.Equal(t, group.String(), record.TargetUUID)
	}
	require.True(t, kinds[model.NotificationGroupAdminAdded])
	require.True(t, kinds[model.NotificationGroupAdminRemoved])
	require.True(t, kinds[model.NotificationGroupMemberRemoved])
}
