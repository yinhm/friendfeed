package server

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func TestCreateGroupAtomicMembershipAndAdmin(t *testing.T) {
	srv := newServiceServer(t)
	actor := createServiceUser(t, srv, "creator")

	resp, err := srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid:   actor.String(),
		Id:          "book-club",
		Name:        "Book Club",
		Description: "reading group",
	})
	require.NoError(t, err)
	require.Equal(t, "group", resp.Type)
	require.Equal(t, "book-club", resp.Id)

	groupUUID, err := uuid.FromString(resp.Uuid)
	require.NoError(t, err)

	isAdmin, err := model.IsGroupAdmin(srv.rdb, groupUUID, actor)
	require.NoError(t, err)
	require.True(t, isAdmin)

	followed, err := srv.rdb.Exists(model.NewKeyFrom(model.Follow.Prefix, actor.Bytes(), groupUUID.Bytes()))
	require.NoError(t, err)
	require.True(t, followed)

	followerExists, err := srv.rdb.Exists(model.NewKeyFrom(model.Follower.Prefix, groupUUID.Bytes(), actor.Bytes()))
	require.NoError(t, err)
	require.True(t, followerExists)

	fetched, err := model.GetProfileFromUserId(srv.rdb, "book-club")
	require.NoError(t, err)
	require.Equal(t, resp.Uuid, fetched.Uuid)
}

func TestCreateGroupRejectsPrivate(t *testing.T) {
	srv := newServiceServer(t)
	actor := createServiceUser(t, srv, "creator2")

	_, err := srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid: actor.String(),
		Id:        "secret-club",
		Name:      "Secret Club",
		Private:   true,
	})
	require.Error(t, err)
}

func TestCreateGroupRejectsDuplicateID(t *testing.T) {
	srv := newServiceServer(t)
	actor := createServiceUser(t, srv, "creator3")

	_, err := srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid: actor.String(),
		Id:        "dup-club",
		Name:      "Dup Club",
	})
	require.NoError(t, err)

	other := createServiceUser(t, srv, "creator4")
	_, err = srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid: other.String(),
		Id:        "dup-club",
		Name:      "Dup Club Again",
	})
	require.Error(t, err)
}

func TestCreateGroupRejectsNonUserActor(t *testing.T) {
	srv := newServiceServer(t)
	group := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{Uuid: group.String(), Id: "existing-group", Type: "group"}))

	_, err := srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid: group.String(),
		Id:        "nested-club",
		Name:      "Nested Club",
	})
	require.Error(t, err)
}

func createTestGroup(t *testing.T, srv *ApiServer, actor uuid.UUID, id string) uuid.UUID {
	t.Helper()
	resp, err := srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid: actor.String(),
		Id:        id,
		Name:      id,
	})
	require.NoError(t, err)
	group, err := uuid.FromString(resp.Uuid)
	require.NoError(t, err)
	return group
}

func TestJoinGroupSelfServiceOnly(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	group := createTestGroup(t, srv, creator, "join-club")

	member := createServiceUser(t, srv, "member1")
	other := createServiceUser(t, srv, "other")

	_, err := srv.JoinGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  other.String(),
		GroupUuid:  group.String(),
		TargetUuid: member.String(),
	})
	require.Error(t, err)

	_, err = srv.JoinGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  member.String(),
		GroupUuid:  group.String(),
		TargetUuid: member.String(),
	})
	require.NoError(t, err)

	isMember, err := model.IsGroupMember(srv.rdb, group, member)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestLeaveGroupSelfServiceOnly(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	group := createTestGroup(t, srv, creator, "leave-club")

	member := createServiceUser(t, srv, "member1")
	_, err := srv.JoinGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  member.String(),
		GroupUuid:  group.String(),
		TargetUuid: member.String(),
	})
	require.NoError(t, err)

	other := createServiceUser(t, srv, "other")
	_, err = srv.LeaveGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  other.String(),
		GroupUuid:  group.String(),
		TargetUuid: member.String(),
	})
	require.Error(t, err)

	_, err = srv.LeaveGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  member.String(),
		GroupUuid:  group.String(),
		TargetUuid: member.String(),
	})
	require.NoError(t, err)

	isMember, err := model.IsGroupMember(srv.rdb, group, member)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestAddGroupAdminRequiresAdminOrSuper(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	group := createTestGroup(t, srv, creator, "admin-club")

	member := createServiceUser(t, srv, "member1")
	_, err := srv.JoinGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  member.String(),
		GroupUuid:  group.String(),
		TargetUuid: member.String(),
	})
	require.NoError(t, err)

	outsider := createServiceUser(t, srv, "outsider")
	_, err = srv.AddGroupAdmin(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  outsider.String(),
		GroupUuid:  group.String(),
		TargetUuid: member.String(),
	})
	require.Error(t, err)

	_, err = srv.AddGroupAdmin(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  creator.String(),
		GroupUuid:  group.String(),
		TargetUuid: member.String(),
	})
	require.NoError(t, err)

	isAdmin, err := model.IsGroupAdmin(srv.rdb, group, member)
	require.NoError(t, err)
	require.True(t, isAdmin)
}

func TestRemoveGroupAdminRejectsLastAdmin(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	group := createTestGroup(t, srv, creator, "sole-admin-club")

	_, err := srv.RemoveGroupAdmin(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  creator.String(),
		GroupUuid:  group.String(),
		TargetUuid: creator.String(),
	})
	require.Error(t, err)
}

func TestRemoveGroupMemberRequiresAdminOrSuper(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	group := createTestGroup(t, srv, creator, "remove-club")

	member := createServiceUser(t, srv, "member1")
	_, err := srv.JoinGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  member.String(),
		GroupUuid:  group.String(),
		TargetUuid: member.String(),
	})
	require.NoError(t, err)

	outsider := createServiceUser(t, srv, "outsider")
	_, err = srv.RemoveGroupMember(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  outsider.String(),
		GroupUuid:  group.String(),
		TargetUuid: member.String(),
	})
	require.Error(t, err)

	_, err = srv.RemoveGroupMember(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  creator.String(),
		GroupUuid:  group.String(),
		TargetUuid: member.String(),
	})
	require.NoError(t, err)

	isMember, err := model.IsGroupMember(srv.rdb, group, member)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestGraphFollowRoutesGroupThroughJoinLeave(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	group := createTestGroup(t, srv, creator, "graph-club")

	member := createServiceUser(t, srv, "member1")
	_, err := srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: member.String(),
		FeedUuid:    group.String(),
		Action:      "follow",
	})
	require.NoError(t, err)

	isMember, err := model.IsGroupMember(srv.rdb, group, member)
	require.NoError(t, err)
	require.True(t, isMember)

	_, err = srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: member.String(),
		FeedUuid:    group.String(),
		Action:      "unfollow",
	})
	require.NoError(t, err)

	isMember, err = model.IsGroupMember(srv.rdb, group, member)
	require.NoError(t, err)
	require.False(t, isMember)

	// The sole admin cannot leave via GraphFollow's unfollow path either.
	_, err = srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: creator.String(),
		FeedUuid:    group.String(),
		Action:      "unfollow",
	})
	require.Error(t, err)
}
