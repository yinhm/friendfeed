package server

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestGraphFollowSerializesWithPrivacyTransition(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "owner")
	requester := createServiceUser(t, srv, "requester")

	// Hold the same mutation lock PostFeedinfo uses, then start a follow.
	// GraphFollow must not read the old public profile while this critical
	// section is in progress; once released it should observe private and
	// create a request rather than a Follow edge.
	srv.profileUpdateMu.Lock()
	type result struct {
		resp *pb.FollowResponse
		err  error
	}
	started := make(chan struct{})
	done := make(chan result, 1)
	go func() {
		close(started)
		resp, err := srv.GraphFollow(context.Background(), &pb.FollowRequest{
			ProfileUuid: requester.String(),
			FeedUuid:    owner.String(),
			Action:      "follow",
		})
		done <- result{resp: resp, err: err}
	}()
	<-started

	select {
	case got := <-done:
		srv.profileUpdateMu.Unlock()
		t.Fatalf("GraphFollow returned while privacy mutation lock was held: resp=%v err=%v", got.resp, got.err)
	case <-time.After(25 * time.Millisecond):
	}

	profile, err := model.GetProfileFromUuid(srv.rdb, owner)
	require.NoError(t, err)
	profile.Private = true
	_, err = model.Profile.Put(srv.rdb, owner.Bytes(), profile)
	require.NoError(t, err)
	srv.profileUpdateMu.Unlock()

	got := <-done
	require.NoError(t, got.err)
	require.NotNil(t, got.resp)
	require.False(t, got.resp.Followed)
	require.True(t, got.resp.Requested)

	following, err := model.IsFollower(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.False(t, following, "privacy transition must not be bypassed by a stale public read")
	pending, err := model.IsFollowRequestPending(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.True(t, pending)
}

func TestPostFeedinfoRetriesCleanupForAlreadyPublicProfile(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "owner")
	requester := createServiceUser(t, srv, "requester")
	makeProfilePrivate(t, srv, owner)
	requestFollow(t, srv, requester, owner)

	// Simulate the failure window where the profile write to public succeeded
	// but follow-request cleanup did not. A later public profile save must
	// retry the idempotent cleanup even though this is no longer a transition.
	profile, err := model.GetProfileFromUuid(srv.rdb, owner)
	require.NoError(t, err)
	profile.Private = false
	_, err = model.Profile.Put(srv.rdb, owner.Bytes(), profile)
	require.NoError(t, err)

	pending, err := model.IsFollowRequestPending(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.True(t, pending)

	_, err = srv.PostFeedinfo(context.Background(), &pb.Feedinfo{
		Uuid:        profile.Uuid,
		Id:          profile.Id,
		Name:        profile.Name,
		Type:        profile.Type,
		Private:     false,
		Picture:     profile.Picture,
		Description: profile.Description,
	})
	require.NoError(t, err)

	pending, err = model.IsFollowRequestPending(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.False(t, pending, "every public profile save must retry stale request cleanup")
}

func TestApproveFollowRequestRejectsPublicTarget(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "owner")
	requester := createServiceUser(t, srv, "requester")
	makeProfilePrivate(t, srv, owner)
	requestFollow(t, srv, requester, owner)

	// Leave a stale request behind while making the target public. Even if
	// cleanup was missed, approval must not turn that stale workflow row into
	// a relationship.
	profile, err := model.GetProfileFromUuid(srv.rdb, owner)
	require.NoError(t, err)
	profile.Private = false
	_, err = model.Profile.Put(srv.rdb, owner.Bytes(), profile)
	require.NoError(t, err)

	_, err = srv.ApproveFollowRequest(context.Background(), &pb.FollowRequestAction{
		ActorUuid:  owner.String(),
		FeedUuid:   owner.String(),
		TargetUuid: requester.String(),
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))

	following, err := model.IsFollower(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.False(t, following)
	pending, err := model.IsFollowRequestPending(srv.rdb, owner, requester)
	require.NoError(t, err)
	require.True(t, pending, "failed approval must not partially consume the stale request")
}

func TestPublicTimelineFailsClosedForUnresolvableEntryTarget(t *testing.T) {
	srv := newPublicTestServer(t)
	author := addPublicTestProfile(t, srv, "author", false)
	entry := postPublicTestEntry(t, srv, author, "legacy public row")
	require.Contains(t, publicFeedEntryIDs(fetchPublicFeed(t, srv, nil)), entry.Id)

	// Keep the materialized Public timeline row but corrupt the entry's stable
	// target identity. From remains a valid legacy author snapshot, so the old
	// fail-open path could still format and render it.
	entry.ProfileUuid = ""
	entry.FeedUuid = "not-a-uuid"
	raw, err := proto.Marshal(entry)
	require.NoError(t, err)
	entryUUID := uuid.Must(uuid.FromString(entry.Id))
	require.NoError(t, srv.rdb.Put(model.Entry.PrefixAppend(entryUUID.Bytes()), raw))

	require.NotContains(t, publicFeedEntryIDs(fetchPublicFeed(t, srv, nil)), entry.Id)
}
