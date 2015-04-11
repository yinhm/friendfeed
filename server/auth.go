package server

import (
	uuid "github.com/satori/go.uuid"
	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
	"golang.org/x/net/context"
)

func (s *ApiServer) Auth(ctx context.Context, user *pb.OAuthUser) (*pb.Profile, error) {
	user, err := store.UpdateOAuthUser(s.mdb, user)
	if err != nil {
		return nil, err
	}

	// exists user
	if user.Uuid != "" {
		uuid1, err := uuid.FromString(user.Uuid)
		if err != nil {
			return nil, err
		}
		return store.GetProfileFromUuid(s.mdb, uuid1)
	}

	return new(pb.Profile), nil
}

func (s *ApiServer) BindUserFeed(ctx context.Context, user *pb.OAuthUser) (*pb.OAuthUser, error) {
	return store.BindOAuthUser(s.mdb, user)
}

func (s *ApiServer) FetchProfile(ctx context.Context, req *pb.ProfileRequest) (*pb.Profile, error) {
	uuid1, err := uuid.FromString(req.Uuid)
	if err != nil {
		return nil, err
	}
	return store.GetProfileFromUuid(s.mdb, uuid1)
}
