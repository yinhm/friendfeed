package server

import (
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"golang.org/x/net/context"
)

func (s *ApiServer) GraphFollow(ctx context.Context, req *pb.FollowRequest) (*pb.FollowResponse, error) {
	logger.Debugf("GraphFollow, <%s, %s>", req.ProfileUuid, req.FeedUuid)

	uuid1, err := uuid.FromString(req.ProfileUuid)
	if err != nil {
		return nil, err
	}
	uuid2, err := uuid.FromString(req.FeedUuid)
	if err != nil {
		return nil, err
	}

	followed := false
	followkey := model.NewKeyFrom(model.Follow.Prefix, uuid1.Bytes(), uuid2.Bytes())
	followerkey := model.NewKeyFrom(model.Follower.Prefix, uuid2.Bytes(), uuid1.Bytes())
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
		followed = s.rdb.Exist(followkey)
	}
	return &pb.FollowResponse{Followed: followed}, nil
}
