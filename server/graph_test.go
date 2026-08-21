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
)

func TestGraphFollowMaintainsBothEdges(t *testing.T) {
	srv := newServiceServer(t)
	db := srv.rdb

	profileUUID := uuid.Must(uuid.NewV4())
	feedUUID := uuid.Must(uuid.NewV4())
	req := &pb.FollowRequest{
		ProfileUuid: profileUUID.String(),
		FeedUuid:    feedUUID.String(),
		Action:      "follow",
	}
	resp, err := srv.GraphFollow(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.Followed)

	followKey := model.NewKeyFrom(model.Follow.Prefix, profileUUID.Bytes(), feedUUID.Bytes())
	followerKey := model.NewKeyFrom(model.Follower.Prefix, feedUUID.Bytes(), profileUUID.Bytes())
	for _, key := range [][]byte{followKey, followerKey} {
		exists, err := db.Exists(key)
		require.NoError(t, err)
		require.True(t, exists)
	}

	req.Action = "unfollow"
	resp, err = srv.GraphFollow(context.Background(), req)
	require.NoError(t, err)
	require.False(t, resp.Followed)
	for _, key := range [][]byte{followKey, followerKey} {
		exists, err := db.Exists(key)
		require.NoError(t, err)
		require.False(t, exists)
	}
}

func TestGraphFollowAddsAtMostOneHundredFeedEntriesAndUnfollowRemovesThem(t *testing.T) {
	srv := newServiceServer(t)
	viewer := createServiceUser(t, srv, "timeline-viewer")
	feed := createServiceUser(t, srv, "timeline-source")
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 105; i++ {
		_, err := srv.PostEntry(context.Background(), &pb.Entry{
			Id: uuid.Must(uuid.NewV4()).String(), ProfileUuid: feed.String(), FeedUuid: feed.String(),
			Date: base.Add(-time.Duration(i) * time.Second).Format(time.RFC3339),
		})
		require.NoError(t, err)
	}

	_, err := srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: viewer.String(), FeedUuid: feed.String(), Action: "follow",
	})
	require.NoError(t, err)
	require.Equal(t, 1, runHomeRebuildTasks(t, srv))
	require.Equal(t, 100, countHomeFeedRows(t, srv, viewer, feed))

	_, err = srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: viewer.String(), FeedUuid: feed.String(), Action: "unfollow",
	})
	require.NoError(t, err)
	require.Equal(t, 1, runHomeRebuildTasks(t, srv))
	require.Zero(t, countHomeFeedRows(t, srv, viewer, feed))
}

func TestHomeFeedTasksConvergeToCurrentFollowEdge(t *testing.T) {
	srv := newServiceServer(t)
	viewer := createServiceUser(t, srv, "rapid-viewer")
	feed := createServiceUser(t, srv, "rapid-source")
	_, err := srv.PostEntry(context.Background(), &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), ProfileUuid: feed.String(), FeedUuid: feed.String(),
		Date: time.Now().UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)

	_, err = srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: viewer.String(), FeedUuid: feed.String(), Action: "follow",
	})
	require.NoError(t, err)
	_, err = srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: viewer.String(), FeedUuid: feed.String(), Action: "unfollow",
	})
	require.NoError(t, err)
	require.Equal(t, 2, runHomeRebuildTasks(t, srv))
	require.Zero(t, countHomeFeedRows(t, srv, viewer, feed), "a stale add task must not restore an unfollowed feed")
}

func countHomeFeedRows(t *testing.T, srv *ApiServer, viewer, feed uuid.UUID) int {
	t.Helper()
	count := 0
	_, err := srv.rdb.ForwardScan(model.TimelineIndexPrefix(viewer), func(_ int, key, _ []byte) error {
		_, entryID, _, err := model.ParseTimelineIndexKey(key)
		if err != nil {
			return err
		}
		entry := new(pb.Entry)
		if err := model.Entry.Get(srv.rdb, entryID.Bytes(), entry); err != nil {
			return err
		}
		if entry.FeedUuid == feed.String() {
			count++
		}
		return nil
	})
	require.NoError(t, err)
	return count
}

func TestGraphFollowRejectsMalformedUUID(t *testing.T) {
	srv := &ApiServer{}

	_, err := srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: "not-a-uuid",
		FeedUuid:    uuid.Must(uuid.NewV4()).String(),
		Action:      "follow",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = srv.GraphFollow(context.Background(), &pb.FollowRequest{
		ProfileUuid: uuid.Must(uuid.NewV4()).String(),
		FeedUuid:    "not-a-uuid",
		Action:      "follow",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
