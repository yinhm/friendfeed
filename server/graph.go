package server

import (
	"context"
	"log/slog"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func (s *ApiServer) GraphFollow(ctx context.Context, req *pb.FollowRequest) (*pb.FollowResponse, error) {
	slog.Debug("GraphFollow", "profile_uuid", req.ProfileUuid, "feed_uuid", req.FeedUuid)

	profileUUID, err := uuid.FromString(req.ProfileUuid)
	if err != nil {
		return nil, err
	}
	feedUUID, err := uuid.FromString(req.FeedUuid)
	if err != nil {
		return nil, err
	}

	followed := false
	followkey := model.NewKeyFrom(model.Follow.Prefix, profileUUID.Bytes(), feedUUID.Bytes())
	followerkey := model.NewKeyFrom(model.Follower.Prefix, feedUUID.Bytes(), profileUUID.Bytes())
	switch req.Action {
	case "follow":
		// follow
		err = s.rdb.Put(followkey, []byte("1"))
		if err != nil {
			return nil, err
		}
		// follower
		err = s.rdb.Put(followerkey, []byte("1"))
		if err != nil {
			return nil, err
		}

		followed = true
	case "unfollow":
		// follow
		s.rdb.Delete(followkey)
		// follower
		s.rdb.Delete(followerkey)
		followed = false
	default:
		// follow
		followed, err = s.rdb.Exists(followkey)
		if err != nil {
			return nil, err
		}
	}
	return &pb.FollowResponse{Followed: followed}, nil
}
