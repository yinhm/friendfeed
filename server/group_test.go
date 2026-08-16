package server

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	_, err := srv.postEntry(context.Background(), entry, true)
	require.NoError(t, err)
}

func TestPublicPostEntryRejectsGroupAsPrincipal(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	group := createTestGroup(t, srv, creator, "no-group-principal")
	entry := &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), Date: "2026-08-16T00:00:00Z",
		ProfileUuid: group.String(), FeedUuid: group.String(), Body: "forged",
	}
	_, err := srv.PostEntry(context.Background(), entry)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	// Changing the destination must not turn a Group UUID into a valid
	// caller identity. The trusted internal escape hatch is self-feed only.
	user := createServiceUser(t, srv, "ordinary-target")
	entry.Id = uuid.Must(uuid.NewV4()).String()
	entry.FeedUuid = user.String()
	_, err = srv.PostEntry(context.Background(), entry)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = srv.postEntry(context.Background(), entry, true)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestPostEntryMintsCanonicalAuthorSnapshot(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	member := createServiceUser(t, srv, "real-member")
	group := createTestGroup(t, srv, creator, "canonical-author")
	require.NoError(t, model.JoinGroup(srv.rdb, group, member))
	entry := &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), Date: "2026-08-16T00:00:00Z",
		ProfileUuid: member.String(), FeedUuid: group.String(),
		From: &pb.Feed{Uuid: creator.String(), Id: "forged", Name: "Forged"},
	}
	posted, err := srv.PostEntry(context.Background(), entry)
	require.NoError(t, err)
	require.Equal(t, member.String(), posted.From.Uuid)
	require.Equal(t, "real-member", posted.From.Id)
	require.Empty(t, posted.From.Name)
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

func TestPrivateGroupAccessControl(t *testing.T) {
	srv := newServiceServer(t)
	admin := createServiceUser(t, srv, "admin")
	member := createServiceUser(t, srv, "member")
	outsider := createServiceUser(t, srv, "outsider")

	// Create a group
	groupResp, err := srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid:   admin.String(),
		Id:          "secret-club",
		Name:        "Secret Club",
		Description: "private group",
	})
	require.NoError(t, err)
	groupUUID, _ := uuid.FromString(groupResp.Uuid)

	// Member joins while the group is still public; docs/group.md rejects
	// joining a private Group until the approval flow exists.
	_, err = srv.JoinGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  member.String(),
		GroupUuid:  groupResp.Uuid,
		TargetUuid: member.String(),
	})
	require.NoError(t, err)

	// Update group to make it private
	profile, err := model.GetProfileFromUuid(srv.rdb, groupUUID)
	require.NoError(t, err)
	profile.Private = true
	_, err = model.Profile.Put(srv.rdb, groupUUID.Bytes(), profile)
	require.NoError(t, err)

	// Joining a private Group is rejected outright.
	_, err = srv.JoinGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid:  outsider.String(),
		GroupUuid:  groupResp.Uuid,
		TargetUuid: outsider.String(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "private Group creation is not yet supported")

	// Post an entry to the private group
	entryUUID := uuid.Must(uuid.NewV4())
	entry := &pb.Entry{
		Id:          entryUUID.String(),
		ProfileUuid: admin.String(),
		FeedUuid:    groupResp.Uuid,
		Body:        "Secret message",
		Date:        "2026-08-16T00:00:00Z",
	}
	entryKey, err := model.PutEntry(srv.rdb, entry)
	require.NoError(t, err)
	require.NotNil(t, entryKey)

	// Test FetchFeed access control
	t.Run("Admin can access private group feed", func(t *testing.T) {
		feed, err := srv.ForwardFetchFeed(context.Background(), &pb.FeedRequest{
			Id:         "secret-club",
			PageSize:   30,
			ViewerUuid: admin.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, feed)
		require.Equal(t, groupResp.Uuid, feed.Uuid)
	})

	t.Run("Member can access private group feed", func(t *testing.T) {
		feed, err := srv.ForwardFetchFeed(context.Background(), &pb.FeedRequest{
			Id:         "secret-club",
			PageSize:   30,
			ViewerUuid: member.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, feed)
	})

	t.Run("Outsider cannot access private group feed", func(t *testing.T) {
		_, err := srv.ForwardFetchFeed(context.Background(), &pb.FeedRequest{
			Id:         "secret-club",
			PageSize:   30,
			ViewerUuid: outsider.String(),
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "access denied")
	})

	t.Run("Unauthenticated cannot access private group feed", func(t *testing.T) {
		_, err := srv.ForwardFetchFeed(context.Background(), &pb.FeedRequest{
			Id:       "secret-club",
			PageSize: 30,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "authentication required")
	})

	t.Run("Outsider cannot access private group feed via cursor paging", func(t *testing.T) {
		_, err := srv.ForwardFetchFeedWithCursor(context.Background(), &pb.FeedRequest{
			Id:           "secret-club",
			PageSize:     30,
			CursorPaging: true,
			ViewerUuid:   outsider.String(),
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "access denied")
	})

	t.Run("Member can access private group feed via cursor paging", func(t *testing.T) {
		feed, err := srv.ForwardFetchFeedWithCursor(context.Background(), &pb.FeedRequest{
			Id:           "secret-club",
			PageSize:     30,
			CursorPaging: true,
			ViewerUuid:   member.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, feed)
		require.Len(t, feed.Entries, 1)
	})

	t.Run("Outsider cannot access private group entry", func(t *testing.T) {
		entryUUID := entry.Id
		_, err := srv.FetchEntry(context.Background(), &pb.EntryRequest{
			Uuid:       entryUUID,
			ViewerUuid: outsider.String(),
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "access denied")
	})

	t.Run("Member can access private group entry", func(t *testing.T) {
		entryUUID := entry.Id
		feed, err := srv.FetchEntry(context.Background(), &pb.EntryRequest{
			Uuid:       entryUUID,
			ViewerUuid: member.String(),
		})
		require.NoError(t, err)
		require.NotNil(t, feed)
		require.Len(t, feed.Entries, 1)
	})

	t.Run("Direct Group metadata and member APIs enforce private visibility", func(t *testing.T) {
		_, err := srv.GetGroup(context.Background(), &pb.GetGroupRequest{
			GroupUuid: groupUUID.String(), ViewerUuid: outsider.String(),
		})
		require.Equal(t, codes.PermissionDenied, status.Code(err))
		_, err = srv.ListGroupMembers(context.Background(), &pb.ListGroupMembersRequest{
			GroupUuid: groupUUID.String(), ViewerUuid: outsider.String(),
		})
		require.Equal(t, codes.PermissionDenied, status.Code(err))

		view, err := srv.GetGroup(context.Background(), &pb.GetGroupRequest{
			GroupUuid: groupUUID.String(), ViewerUuid: member.String(),
		})
		require.NoError(t, err)
		require.True(t, view.IsMember)
		members, err := srv.ListGroupMembers(context.Background(), &pb.ListGroupMembersRequest{
			GroupUuid: groupUUID.String(), ViewerUuid: member.String(),
		})
		require.NoError(t, err)
		require.NotEmpty(t, members.Members)
	})
}

// runHomeRebuildTasks claims and executes every pending home.rebuild task,
// standing in for the background worker.
func runHomeRebuildTasks(t *testing.T, srv *ApiServer) int {
	t.Helper()
	tasks, err := srv.tasks.Claim(context.Background(), "test-worker", []string{homeRebuildTaskType}, 10)
	require.NoError(t, err)
	for _, task := range tasks {
		require.NoError(t, srv.handleHomeRebuildTask(context.Background(), task))
	}
	return len(tasks)
}

func fetchHomeEntryIDs(t *testing.T, srv *ApiServer, viewer uuid.UUID) []string {
	t.Helper()
	feed, err := srv.FetchFeed(context.Background(), &pb.FeedRequest{
		ProfileUuid:  viewer.String(),
		CursorPaging: true,
		PageSize:     30,
	})
	require.NoError(t, err)
	ids := make([]string, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		ids = append(ids, entry.Id)
	}
	return ids
}

// docs/group.md acceptance: after a membership change the Home timeline must
// converge, not just change future fanout. Join/Leave enqueue a home.rebuild
// task in the same batch as the membership mutation.
func TestGroupMembershipChangeConvergesHomeTimeline(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	member := createServiceUser(t, srv, "member")
	group := createTestGroup(t, srv, creator, "timeline-club")

	entry, err := postGroupEntry(t, srv, creator, group)
	require.NoError(t, err)

	// Join enqueues exactly one home.rebuild task for the joining user.
	_, err = srv.JoinGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid: member.String(), GroupUuid: group.String(), TargetUuid: member.String(),
	})
	require.NoError(t, err)
	require.Equal(t, 1, runHomeRebuildTasks(t, srv), "JoinGroup must enqueue a home.rebuild task")
	require.Contains(t, fetchHomeEntryIDs(t, srv, member), entry.Id, "Group content must appear in Home after join rebuild")

	// Leave enqueues another task; after it runs the Group rows are gone.
	_, err = srv.LeaveGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid: member.String(), GroupUuid: group.String(), TargetUuid: member.String(),
	})
	require.NoError(t, err)
	require.Equal(t, 1, runHomeRebuildTasks(t, srv), "LeaveGroup must enqueue a home.rebuild task")
	require.NotContains(t, fetchHomeEntryIDs(t, srv, member), entry.Id, "Group content must leave Home after leave rebuild")
}

func TestDeleteGroupSoftDeleteClosesGroup(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	member := createServiceUser(t, srv, "member")
	outsider := createServiceUser(t, srv, "outsider")
	group := createTestGroup(t, srv, creator, "doomed-club")
	require.NoError(t, model.JoinGroup(srv.rdb, group, member))

	// A non-admin cannot delete the Group.
	_, err := srv.DeleteGroup(context.Background(), &pb.DeleteGroupRequest{
		ActorUuid: outsider.String(), GroupUuid: group.String(),
	})
	require.Error(t, err)

	// The last admin may delete the whole Group.
	_, err = srv.DeleteGroup(context.Background(), &pb.DeleteGroupRequest{
		ActorUuid: creator.String(), GroupUuid: group.String(),
	})
	require.NoError(t, err)

	// Join is immediately rejected.
	_, err = srv.JoinGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid: outsider.String(), GroupUuid: group.String(), TargetUuid: outsider.String(),
	})
	require.Error(t, err)

	// Posting is immediately rejected.
	_, err = postGroupEntry(t, srv, creator, group)
	require.Error(t, err)

	// Reads report the Group as gone.
	_, err = srv.GetGroup(context.Background(), &pb.GetGroupRequest{GroupUuid: group.String()})
	require.Error(t, err)

	// The deleted Group leaves every member's group list.
	resp, err := srv.ListUserGroups(context.Background(), &pb.ListUserGroupsRequest{UserUuid: member.String()})
	require.NoError(t, err)
	require.Empty(t, resp.Groups)
}

// docs/group.md acceptance: soft-deleting the account of a sole Group admin
// must be rejected and name the blocking Groups.
func TestMarkDeleteRejectsSoleGroupAdmin(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	member := createServiceUser(t, srv, "member")
	group := createTestGroup(t, srv, creator, "guarded-club")

	_, err := srv.MarkDelete("creator")
	require.Error(t, err)
	require.Contains(t, err.Error(), "guarded-club", "the rejection must list the blocking Group")

	// Hand over admin first; deletion then succeeds.
	require.NoError(t, model.JoinGroup(srv.rdb, group, member))
	require.NoError(t, model.AddGroupAdmin(srv.rdb, group, member))
	_, err = srv.MarkDelete("creator")
	require.NoError(t, err)
	_, err = model.GetProfileFromUuid(srv.rdb, creator)
	require.ErrorIs(t, err, model.ErrProfileDeleted)
	isMember, err := model.IsGroupMember(srv.rdb, group, creator)
	require.NoError(t, err)
	require.False(t, isMember)
	isAdmin, err := model.IsGroupAdmin(srv.rdb, group, creator)
	require.NoError(t, err)
	require.False(t, isAdmin)
	follower, err := srv.rdb.Exists(model.NewKeyFrom(model.Follower.Prefix, group.Bytes(), creator.Bytes()))
	require.NoError(t, err)
	require.False(t, follower)
}

func TestGraphFollowCannotReviveDeletedGroup(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	member := createServiceUser(t, srv, "member")
	group := createTestGroup(t, srv, creator, "deleted-follow")
	_, err := srv.DeleteGroup(context.Background(), &pb.DeleteGroupRequest{
		ActorUuid: creator.String(), GroupUuid: group.String(),
	})
	require.NoError(t, err)
	_, err = srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: member.String(), FeedUuid: group.String(), Action: "follow",
	})
	require.ErrorIs(t, err, model.ErrProfileDeleted)
	exists, err := srv.rdb.Exists(model.NewKeyFrom(model.Follow.Prefix, member.Bytes(), group.Bytes()))
	require.NoError(t, err)
	require.False(t, exists)
}

func TestListGroupMembersPagesWithAdminFlags(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	memberA := createServiceUser(t, srv, "member-a")
	memberB := createServiceUser(t, srv, "member-b")
	group := createTestGroup(t, srv, creator, "member-club")
	require.NoError(t, model.JoinGroup(srv.rdb, group, memberA))
	require.NoError(t, model.JoinGroup(srv.rdb, group, memberB))

	page1, err := srv.ListGroupMembers(context.Background(), &pb.ListGroupMembersRequest{
		GroupUuid: group.String(), Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, page1.Members, 2)
	require.NotEmpty(t, page1.NextCursor)

	page2, err := srv.ListGroupMembers(context.Background(), &pb.ListGroupMembersRequest{
		GroupUuid: group.String(), Limit: 2, Cursor: page1.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, page2.Members, 1)
	require.Empty(t, page2.NextCursor)

	seen := map[string]bool{}
	adminFlags := map[string]bool{}
	for _, m := range append(page1.Members, page2.Members...) {
		require.False(t, seen[m.Profile.Uuid], "pages must not repeat members")
		seen[m.Profile.Uuid] = true
		adminFlags[m.Profile.Uuid] = m.IsAdmin
	}
	require.Len(t, seen, 3, "pages must not drop members")
	require.True(t, adminFlags[creator.String()])
	require.False(t, adminFlags[memberA.String()])
	require.False(t, adminFlags[memberB.String()])
}

func TestListGroupMembersRejectsNonGroupProfile(t *testing.T) {
	srv := newServiceServer(t)
	user := createServiceUser(t, srv, "plain-user")
	_, err := srv.ListGroupMembers(context.Background(), &pb.ListGroupMembersRequest{GroupUuid: user.String()})
	require.Equal(t, codes.NotFound, status.Code(err))
}

// A cursor whose edge was deleted between pages must resume at its successor,
// not stall or restart the iteration.
func TestListUserGroupsCursorSurvivesDeletedEdge(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	user := createServiceUser(t, srv, "reader")
	groups := []uuid.UUID{
		createTestGroup(t, srv, creator, "cursor-a"),
		createTestGroup(t, srv, creator, "cursor-b"),
		createTestGroup(t, srv, creator, "cursor-c"),
	}
	for _, group := range groups {
		require.NoError(t, model.JoinGroup(srv.rdb, group, user))
	}

	page1, err := srv.ListUserGroups(context.Background(), &pb.ListUserGroupsRequest{
		UserUuid: user.String(), Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, page1.Groups, 2)
	require.NotEmpty(t, page1.NextCursor)

	// Delete the cursor edge before requesting the next page.
	cursorUUID := uuid.Must(uuid.FromString(page1.NextCursor))
	require.NoError(t, srv.rdb.Delete(model.NewKeyFrom(model.Follow.Prefix, user.Bytes(), cursorUUID.Bytes())))

	page2, err := srv.ListUserGroups(context.Background(), &pb.ListUserGroupsRequest{
		UserUuid: user.String(), Limit: 2, Cursor: page1.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, page2.Groups, 1, "the remaining Group must still be returned")
	require.Empty(t, page2.NextCursor)
	require.NotContains(t, []string{page1.Groups[0].Uuid, page1.Groups[1].Uuid}, page2.Groups[0].Uuid)
}

// Hitting the edge-scan budget must still make progress: pages keep carrying
// an advancing next_cursor and iteration terminates without duplicates.
func TestListUserGroupsScanCapKeepsProgress(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	user := createServiceUser(t, srv, "reader")
	group := createTestGroup(t, srv, creator, "needle-club")
	require.NoError(t, model.JoinGroup(srv.rdb, group, user))

	// More dangling Follow edges than one call's scan budget; their target
	// Profiles deliberately do not exist and must be skipped.
	for i := 0; i < maxMembershipEdgeScan+1; i++ {
		feed := uuid.Must(uuid.NewV4())
		require.NoError(t, srv.rdb.Set(model.NewKeyFrom(model.Follow.Prefix, user.Bytes(), feed.Bytes()), []byte("1")))
	}

	found := 0
	cursor := ""
	for page := 0; page < 5; page++ {
		resp, err := srv.ListUserGroups(context.Background(), &pb.ListUserGroupsRequest{
			UserUuid: user.String(), Limit: 200, Cursor: cursor,
		})
		require.NoError(t, err)
		for _, g := range resp.Groups {
			require.Equal(t, group.String(), g.Uuid, "only the real Group may be returned")
			found++
		}
		if resp.NextCursor == "" {
			break
		}
		require.NotEqual(t, cursor, resp.NextCursor, "cursor must advance between pages")
		cursor = resp.NextCursor
	}
	require.Equal(t, 1, found, "the Group must be returned exactly once across pages")
}

// docs/group.md: a stale Home row from a private Group must be revalidated
// on read even when the Leave-triggered rebuild has not run yet.
func TestHomeReadPathFiltersStalePrivateGroupRows(t *testing.T) {
	srv := newServiceServer(t)
	creator := createServiceUser(t, srv, "creator")
	member := createServiceUser(t, srv, "member")
	group := createTestGroup(t, srv, creator, "ephemeral-club")

	entry, err := postGroupEntry(t, srv, creator, group)
	require.NoError(t, err)

	_, err = srv.JoinGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid: member.String(), GroupUuid: group.String(), TargetUuid: member.String(),
	})
	require.NoError(t, err)
	runHomeRebuildTasks(t, srv)
	require.Contains(t, fetchHomeEntryIDs(t, srv, member), entry.Id)

	// Turn the Group private; the member still sees the row.
	profile, err := model.GetProfileFromUuid(srv.rdb, group)
	require.NoError(t, err)
	profile.Private = true
	_, err = model.Profile.Put(srv.rdb, group.Bytes(), profile)
	require.NoError(t, err)
	require.Contains(t, fetchHomeEntryIDs(t, srv, member), entry.Id)

	// Leave without running the rebuild task: the row is stale on disk, and
	// the read path must filter it anyway.
	_, err = srv.LeaveGroup(context.Background(), &pb.GroupMembershipRequest{
		ActorUuid: member.String(), GroupUuid: group.String(), TargetUuid: member.String(),
	})
	require.NoError(t, err)
	require.NotContains(t, fetchHomeEntryIDs(t, srv, member), entry.Id,
		"a stale private-Group row must not be readable after losing membership")
}

// docs/group.md: private-Group content must not leak through Search results.
func TestSearchFiltersPrivateGroupEntries(t *testing.T) {
	srv := newServiceServer(t)

	// Swap the mock index for a real bleve index; PutEntry indexes through
	// the same global.
	idx, err := search.OpenIndex(t.TempDir())
	require.NoError(t, err)
	defer idx.Close()
	prevIndexer := search.Indexer
	search.Indexer = idx
	defer func() { search.Indexer = prevIndexer }()

	creator := createServiceUser(t, srv, "creator")
	member := createServiceUser(t, srv, "member")
	outsider := createServiceUser(t, srv, "outsider")
	group := createTestGroup(t, srv, creator, "cloak-club")
	require.NoError(t, model.JoinGroup(srv.rdb, group, member))

	_, err = model.PutEntry(srv.rdb, &pb.Entry{
		Id:          uuid.NewV5(uuid.NamespaceURL, "cloak-probe").String(),
		Date:        "2026-08-16T00:00:00Z",
		Body:        "cloakprobe secret",
		ProfileUuid: creator.String(),
		FeedUuid:    group.String(),
	})
	require.NoError(t, err)

	// Turn the Group private after the entry was indexed.
	profile, err := model.GetProfileFromUuid(srv.rdb, group)
	require.NoError(t, err)
	profile.Private = true
	_, err = model.Profile.Put(srv.rdb, group.Bytes(), profile)
	require.NoError(t, err)

	searchFor := func(viewer string) *pb.Feed {
		feed, err := srv.Search(context.Background(), &pb.SearchRequest{Query: "cloakprobe", PageSize: 10, ViewerUuid: viewer})
		require.NoError(t, err)
		return feed
	}
	require.Len(t, searchFor(member.String()).Entries, 1, "members keep search visibility")
	require.Empty(t, searchFor(outsider.String()).Entries, "outsiders must not see private-Group hits")
	require.Empty(t, searchFor("").Entries, "anonymous search must not see private-Group hits")
}
