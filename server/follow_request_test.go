package server

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func makeProfilePrivate(t *testing.T, srv *ApiServer, feed uuid.UUID) {
	t.Helper()
	profile, err := model.GetProfileFromUuid(srv.rdb, feed)
	require.NoError(t, err)
	profile.Private = true
	_, err = model.Profile.Put(srv.rdb, feed.Bytes(), profile)
	require.NoError(t, err)
}

func requestFollow(t *testing.T, srv *ApiServer, actor, feed uuid.UUID) *pb.RequestFollowResponse {
	t.Helper()
	resp, err := srv.RequestFollow(context.Background(), &pb.RequestFollowRequest{
		ActorUuid: actor.String(),
		FeedUuid:  feed.String(),
	})
	require.NoError(t, err)
	return resp
}

func approveFollow(t *testing.T, srv *ApiServer, actor, feed, target uuid.UUID) error {
	t.Helper()
	_, err := srv.ApproveFollowRequest(context.Background(), &pb.FollowRequestAction{
		ActorUuid:  actor.String(),
		FeedUuid:   feed.String(),
		TargetUuid: target.String(),
	})
	return err
}

func TestFollowRequestUserFeedLifecycle(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "owner")
	requester := createServiceUser(t, srv, "requester")
	makeProfilePrivate(t, srv, owner)

	resp := requestFollow(t, srv, requester, owner)
	require.True(t, resp.Requested)

	// Idempotent re-request.
	resp = requestFollow(t, srv, requester, owner)
	require.True(t, resp.Requested)

	list, err := srv.ListFollowRequests(context.Background(), &pb.ListFollowRequestsRequest{
		ActorUuid: owner.String(),
		FeedUuid:  owner.String(),
	})
	require.NoError(t, err)
	require.Len(t, list.Requests, 1)
	require.Equal(t, requester.String(), list.Requests[0].Requester.Uuid)
	require.NotEmpty(t, list.Requests[0].RequestedAt)

	require.NoError(t, approveFollow(t, srv, owner, owner, requester))

	following, err := model.IsFollower(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.True(t, following)

	pending, err := model.IsFollowRequestPending(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestRequestFollowRequiresPrivateTarget(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "owner")
	requester := createServiceUser(t, srv, "requester")

	_, err := srv.RequestFollow(context.Background(), &pb.RequestFollowRequest{
		ActorUuid: requester.String(),
		FeedUuid:  owner.String(),
	})
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestApproveFollowRequestAuthorization(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "owner")
	requester := createServiceUser(t, srv, "requester")
	outsider := createServiceUser(t, srv, "outsider")
	makeProfilePrivate(t, srv, owner)
	requestFollow(t, srv, requester, owner)

	// A third party cannot approve.
	require.Error(t, approveFollow(t, srv, outsider, owner, requester))
	// The requester cannot approve their own request.
	require.Error(t, approveFollow(t, srv, requester, owner, requester))

	following, err := model.IsFollower(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.False(t, following)
}

func TestRejectFollowRequestAllowsReRequest(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "owner")
	requester := createServiceUser(t, srv, "requester")
	makeProfilePrivate(t, srv, owner)
	requestFollow(t, srv, requester, owner)

	_, err := srv.RejectFollowRequest(context.Background(), &pb.FollowRequestAction{
		ActorUuid:  owner.String(),
		FeedUuid:   owner.String(),
		TargetUuid: requester.String(),
	})
	require.NoError(t, err)

	pending, err := model.IsFollowRequestPending(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.False(t, pending)

	// Rejection does not blacklist: a new request is accepted.
	resp := requestFollow(t, srv, requester, owner)
	require.True(t, resp.Requested)
}

func TestPrivateUserFeedReadClosure(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "owner")
	outsider := createServiceUser(t, srv, "outsider")

	entryID := uuid.Must(uuid.NewV4())
	_, err := srv.PostEntry(context.Background(), &pb.Entry{
		Id:          entryID.String(),
		ProfileUuid: owner.String(),
		Body:        "private note",
		Date:        "2026-08-21T00:00:00Z",
	})
	require.NoError(t, err)
	makeProfilePrivate(t, srv, owner)

	// Anonymous and outsider reads are denied.
	for _, viewer := range []string{"", outsider.String()} {
		_, err = srv.FetchFeed(context.Background(), &pb.FeedRequest{Id: "owner", ViewerUuid: viewer})
		require.Equal(t, codes.PermissionDenied, status.Code(err), "FetchFeed viewer=%q", viewer)
		_, err = srv.FetchEntry(context.Background(), &pb.EntryRequest{Uuid: entryID.String(), ViewerUuid: viewer})
		require.Equal(t, codes.PermissionDenied, status.Code(err), "FetchEntry viewer=%q", viewer)
	}

	// The owner reads their own feed.
	_, err = srv.FetchFeed(context.Background(), &pb.FeedRequest{Id: "owner", ViewerUuid: owner.String()})
	require.NoError(t, err)

	// An approved follower reads it too.
	requestFollow(t, srv, outsider, owner)
	require.NoError(t, approveFollow(t, srv, owner, owner, outsider))
	_, err = srv.FetchFeed(context.Background(), &pb.FeedRequest{Id: "owner", ViewerUuid: outsider.String()})
	require.NoError(t, err)
	_, err = srv.FetchEntry(context.Background(), &pb.EntryRequest{Uuid: entryID.String(), ViewerUuid: outsider.String()})
	require.NoError(t, err)
}

func TestGraphFollowPrivateRoutesToRequest(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "owner")
	requester := createServiceUser(t, srv, "requester")
	makeProfilePrivate(t, srv, owner)

	resp, err := srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: requester.String(),
		FeedUuid:    owner.String(),
		Action:      "follow",
	})
	require.NoError(t, err)
	require.False(t, resp.Followed)
	require.True(t, resp.Requested)

	following, err := model.IsFollower(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.False(t, following)

	pending, err := model.IsFollowRequestPending(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.True(t, pending)
}

func TestGroupFollowRequestApproval(t *testing.T) {
	srv := newServiceServer(t)
	admin := createServiceUser(t, srv, "admin")
	member := createServiceUser(t, srv, "member")
	outsider := createServiceUser(t, srv, "outsider")

	groupResp, err := srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid: admin.String(),
		Id:        "private-club",
		Name:      "Private Club",
		Private:   true,
	})
	require.NoError(t, err)
	groupUUID, _ := uuid.FromString(groupResp.Uuid)

	requestFollow(t, srv, member, groupUUID)

	// A non-admin member of nothing cannot approve.
	require.Error(t, approveFollow(t, srv, outsider, groupUUID, member))
	// Even the requester cannot self-approve.
	require.Error(t, approveFollow(t, srv, member, groupUUID, member))

	require.NoError(t, approveFollow(t, srv, admin, groupUUID, member))
	isMember, err := model.IsGroupMember(srv.rdb, groupUUID, member)
	require.NoError(t, err)
	require.True(t, isMember)

	// GetGroup reports the viewer's pending state.
	requestFollow(t, srv, outsider, groupUUID)
	view, err := srv.GetGroup(context.Background(), &pb.GetGroupRequest{
		GroupUuid:  groupResp.Uuid,
		ViewerUuid: outsider.String(),
	})
	require.NoError(t, err)
	require.True(t, view.HasPendingRequest)
	require.False(t, view.IsMember)
}
