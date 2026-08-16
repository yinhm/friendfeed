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

func postGroupEntry(t *testing.T, srv *ApiServer, poster, group uuid.UUID) (*pb.Entry, error) {
	t.Helper()
	name := poster.String() + "/" + group.String() + "/" + uuid.Must(uuid.NewV4()).String()
	entry := &pb.Entry{
		Id:          uuid.NewV5(uuid.NamespaceURL, name).String(),
		Date:        "2026-08-16T00:00:00Z",
		Body:        "hello group",
		ProfileUuid: poster.String(),
		FeedUuid:    group.String(),
	}
	return srv.PostEntry(context.Background(), entry)
}

func TestPostEntryRejectsNonMemberPostingToGroup(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	outsider := createServiceUser(t, srv, "outsider")
	group := createTestGroup(t, srv, creator, "posters-club")

	_, err := postGroupEntry(t, srv, outsider, group)
	require.Error(t, err)
}

func TestPostEntryAllowsMemberPostingToGroup(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	member := createServiceUser(t, srv, "member")
	group := createTestGroup(t, srv, creator, "posters-club2")

	require.NoError(t, model.JoinGroup(srv.rdb, group, member))

	entry, err := postGroupEntry(t, srv, member, group)
	require.NoError(t, err)
	require.Equal(t, group.String(), entry.FeedUuid)
}

func TestPostEntryAllowsGroupAdminAndSuperEvenWithoutMembership(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	group := createTestGroup(t, srv, creator, "posters-club3")

	// Creator is already a member and admin from CreateGroup.
	_, err := postGroupEntry(t, srv, creator, group)
	require.NoError(t, err)

	super := createServiceUser(t, srv, "super")
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{Uuid: super.String(), Id: "super", Type: "user", IsSuper: true}))
	_, err = postGroupEntry(t, srv, super, group)
	require.NoError(t, err)
}

func TestPostEntryExemptsGroupSelfPostFromMembershipCheck(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	group := createTestGroup(t, srv, creator, "posters-club4")

	// FeedService imports post as the Group itself (ProfileUuid == FeedUuid);
	// this must succeed even though a Group is never "a member of itself".
	entry := &pb.Entry{
		Id:          uuid.NewV5(uuid.NamespaceURL, group.String()+"/self-post").String(),
		Date:        "2026-08-16T00:00:00Z",
		Body:        "imported item",
		ProfileUuid: group.String(),
		FeedUuid:    group.String(),
		Via:         &pb.Via{Name: "Example Service", Url: "https://example.com"},
	}
	_, err := srv.PostEntry(context.Background(), entry)
	require.NoError(t, err)
}

func TestDeleteEntryAllowsGroupAdminButNotOrdinaryMember(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	member := createServiceUser(t, srv, "member")
	group := createTestGroup(t, srv, creator, "moderators-club")
	require.NoError(t, model.JoinGroup(srv.rdb, group, member))

	entry, err := postGroupEntry(t, srv, member, group)
	require.NoError(t, err)

	// A different plain member may not delete another member's Entry.
	otherMember := createServiceUser(t, srv, "other-member")
	require.NoError(t, model.JoinGroup(srv.rdb, group, otherMember))
	_, err = srv.DeleteEntry(context.Background(), &pb.EntryRequest{Uuid: entry.Id, User: otherMember.String()})
	require.Error(t, err)

	// The Group admin (creator) may delete it despite not being the author.
	_, err = srv.DeleteEntry(context.Background(), &pb.EntryRequest{Uuid: entry.Id, User: creator.String()})
	require.NoError(t, err)
}

func TestUpdateGroupRequiresAdmin(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	member := createServiceUser(t, srv, "member")
	outsider := createServiceUser(t, srv, "outsider")
	group := createTestGroup(t, srv, creator, "editable-club")
	require.NoError(t, model.JoinGroup(srv.rdb, group, member))

	updateReq := &pb.UpdateGroupRequest{
		GroupUuid:   group.String(),
		Name:        "Renamed Club",
		Description: "new description",
		Picture:     "https://example.com/new.png",
	}

	// Outsider cannot edit.
	updateReq.ActorUuid = outsider.String()
	_, err := srv.UpdateGroup(context.Background(), updateReq)
	require.Error(t, err)

	// Plain member cannot edit.
	updateReq.ActorUuid = member.String()
	_, err = srv.UpdateGroup(context.Background(), updateReq)
	require.Error(t, err)

	// Admin can edit.
	updateReq.ActorUuid = creator.String()
	updated, err := srv.UpdateGroup(context.Background(), updateReq)
	require.NoError(t, err)
	require.Equal(t, "Renamed Club", updated.Name)
	require.Equal(t, "new description", updated.Description)
	require.Equal(t, "https://example.com/new.png", updated.Picture)
}

func TestListUserGroupsFiltersAndPaginates(t *testing.T) {
	srv := newServiceServer(t)
	user := createServiceUser(t, srv, "alice")
	otherUser := createServiceUser(t, srv, "bob")

	// Create 3 groups that user joins
	group1 := createTestGroup(t, srv, user, "group-a")
	group2 := createTestGroup(t, srv, user, "group-b")
	group3 := createTestGroup(t, srv, user, "group-c")

	// User also follows other users (should be filtered out)
	_, err := srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: user.String(),
		FeedUuid:    otherUser.String(),
		Action:      "follow",
	})
	require.NoError(t, err)

	// User joins their own groups (already member via CreateGroup)
	// Test basic list
	resp, err := srv.ListUserGroups(context.Background(), &pb.ListUserGroupsRequest{
		UserUuid: user.String(),
		Limit:    10,
	})
	require.NoError(t, err)
	require.Len(t, resp.Groups, 3)
	require.Empty(t, resp.NextCursor)

	// Check returned groups are correct type
	groupIDs := map[uuid.UUID]bool{group1: true, group2: true, group3: true}
	for _, g := range resp.Groups {
		gUUID, err := uuid.FromString(g.Uuid)
		require.NoError(t, err)
		require.True(t, groupIDs[gUUID], "unexpected group %s", g.Uuid)
		require.Equal(t, "group", g.Type)
	}

	// Test pagination with limit=1
	resp, err = srv.ListUserGroups(context.Background(), &pb.ListUserGroupsRequest{
		UserUuid: user.String(),
		Limit:    1,
	})
	require.NoError(t, err)
	require.Len(t, resp.Groups, 1)
	require.NotEmpty(t, resp.NextCursor)

	// Continue pagination
	resp2, err := srv.ListUserGroups(context.Background(), &pb.ListUserGroupsRequest{
		UserUuid: user.String(),
		Limit:    1,
		Cursor:   resp.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, resp2.Groups, 1)
	require.NotEmpty(t, resp2.NextCursor)
	require.NotEqual(t, resp.Groups[0].Uuid, resp2.Groups[0].Uuid)

	// Final page
	resp3, err := srv.ListUserGroups(context.Background(), &pb.ListUserGroupsRequest{
		UserUuid: user.String(),
		Limit:    10,
		Cursor:   resp2.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, resp3.Groups, 1)
	require.Empty(t, resp3.NextCursor)
}

func TestListUserGroupsSkipsDeletedProfiles(t *testing.T) {
	srv := newServiceServer(t)
	user := createServiceUser(t, srv, "alice")

	group1 := createTestGroup(t, srv, user, "active-group")
	group2 := createTestGroup(t, srv, user, "deleted-group")

	// Simulate deleted profile by removing it from Profile table while Follow edge remains
	profileKey := model.Profile.PrefixAppend(group2.Bytes())
	require.NoError(t, srv.rdb.Delete(profileKey))

	resp, err := srv.ListUserGroups(context.Background(), &pb.ListUserGroupsRequest{
		UserUuid: user.String(),
	})
	require.NoError(t, err)
	require.Len(t, resp.Groups, 1)
	require.Equal(t, group1.String(), resp.Groups[0].Uuid)
}
