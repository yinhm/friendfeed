package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func TestBootstrapGroupAdminRepairsOnlyAdminlessGroup(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "orphan-group-member")
	group := createTestGroup(t, srv, creator, "orphan-group")
	adminKey, err := model.GroupAdminKey(group, creator)
	require.NoError(t, err)
	require.NoError(t, srv.rdb.Delete(adminKey))

	request := &pb.CommandRequest{
		Command: "BootstrapGroupAdmin",
		Arg1:    group.String(),
		Arg2:    creator.String(),
	}
	_, err = srv.Command(context.Background(), request)
	require.NoError(t, err)
	isAdmin, err := model.IsGroupAdmin(srv.rdb, group, creator)
	require.NoError(t, err)
	require.True(t, isAdmin)

	_, err = srv.Command(context.Background(), request)
	require.ErrorContains(t, err, "already has an administrator")
}

func TestBootstrapGroupAdminRequiresExistingMembership(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "orphan-group-creator")
	outsider := createServiceUser(t, srv, "orphan-group-outsider")
	group := createTestGroup(t, srv, creator, "orphan-no-member")
	adminKey, err := model.GroupAdminKey(group, creator)
	require.NoError(t, err)
	require.NoError(t, srv.rdb.Delete(adminKey))

	_, err = srv.Command(context.Background(), &pb.CommandRequest{
		Command: "BootstrapGroupAdmin",
		Arg1:    group.String(),
		Arg2:    outsider.String(),
	})
	require.ErrorContains(t, err, "must already be a Group member")
}
