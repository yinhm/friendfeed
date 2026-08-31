package server

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestInspectFeedIncludesDeletedProfileWithoutEntryBodies(t *testing.T) {
	srv := newServiceServer(t)
	feed := uuid.Must(uuid.NewV4())
	entry := uuid.Must(uuid.NewV4())
	now := time.Now().UTC().Truncate(time.Second)
	profile := &pb.Profile{Uuid: feed.String(), Id: "inspect-deleted", Name: "Deleted", Type: "user"}
	require.NoError(t, model.UpdateProfile(srv.rdb, profile))
	_, err := model.PutEntry(srv.rdb, &pb.Entry{
		Id: entry.String(), ProfileUuid: feed.String(), Date: now.Format(time.RFC3339), Body: "secret body",
	})
	require.NoError(t, err)
	profile.Deleted = true
	require.NoError(t, model.UpdateProfile(srv.rdb, profile))

	response, err := srv.InspectFeed(context.Background(), &pb.InspectFeedRequest{Feed: "inspect-deleted", EntryLimit: 10})
	require.NoError(t, err)
	require.True(t, response.Profile.Deleted)
	require.True(t, response.UserMapConsistent)
	require.EqualValues(t, 1, response.EntryCount)
	require.Len(t, response.Entries, 1)
	require.Equal(t, entry.String(), response.Entries[0].Uuid)
}

func TestUpdateFeedStateIsAtomicAndClearsRequestsWhenPublic(t *testing.T) {
	srv := newServiceServer(t)
	first := createServiceUser(t, srv, "privacy-first")
	second := createServiceUser(t, srv, "privacy-second")
	requester := createServiceUser(t, srv, "privacy-requester")
	firstProfile, err := model.GetProfileFromUuid(srv.rdb, first)
	require.NoError(t, err)
	firstProfile.Private = true
	require.NoError(t, model.UpdateProfile(srv.rdb, firstProfile))

	// Stage a real pending request through the domain helper.
	require.NoError(t, srv.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		return model.StageRequestFollow(srv.rdb, batch, first, requester, time.Now().UTC())
	}))

	private := true
	preview, err := srv.UpdateFeedState(context.Background(), &pb.UpdateFeedStateRequest{
		Feeds: []string{"privacy-first", "privacy-second", "privacy-second"},
		Patch: &pb.FeedStatePatch{Private: &private}, DryRun: true,
	})
	require.NoError(t, err)
	require.Len(t, preview.Changes, 2)
	unchangedSecond, err := model.GetProfileFromUuid(srv.rdb, second)
	require.NoError(t, err)
	require.False(t, unchangedSecond.Private, "dry-run must not write")

	group := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{Uuid: group.String(), Id: "privacy-group", Type: "group"}))
	_, err = srv.UpdateFeedState(context.Background(), &pb.UpdateFeedStateRequest{
		Feeds: []string{"privacy-second", "privacy-group"}, Patch: &pb.FeedStatePatch{Private: &private},
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	secondProfile, err := model.GetProfileFromUuid(srv.rdb, second)
	require.NoError(t, err)
	require.False(t, secondProfile.Private, "invalid batch must not partially apply")

	public := false
	response, err := srv.UpdateFeedState(context.Background(), &pb.UpdateFeedStateRequest{
		Feeds: []string{"privacy-first", "privacy-second"}, Patch: &pb.FeedStatePatch{Private: &public},
	})
	require.NoError(t, err)
	require.Len(t, response.Changes, 2)
	pending, err := model.IsFollowRequestPending(srv.rdb, first, requester)
	require.NoError(t, err)
	require.False(t, pending)
}
