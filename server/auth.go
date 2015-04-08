package server

import (
	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
	"golang.org/x/net/context"
)

func (s *ApiServer) Auth(ctx context.Context, user *pb.OAuthUser) (*pb.Feedinfo, error) {
	user, err := store.UpdateOAuthUser(s.mdb, user)
	if err != nil {
		return nil, err
	}

	// exists user
	if user.Uuid != "" {
		return store.GetFeedinfo(s.mdb, user.Uuid)
	}

	return new(pb.Feedinfo), nil
}

func (s *ApiServer) BindUserFeed(ctx context.Context, user *pb.OAuthUser) (*pb.OAuthUser, error) {
	return store.BindOAuthUser(s.mdb, user)
}
