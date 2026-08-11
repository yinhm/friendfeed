package server

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestGraphFollowMaintainsBothEdges(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	srv := &ApiServer{rdb: db}

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
