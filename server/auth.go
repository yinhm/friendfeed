package server

import (
	"time"

	uuid "github.com/satori/go.uuid"
	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
	"golang.org/x/net/context"
)

func (s *ApiServer) PutOAuth(ctx context.Context, authinfo *pb.OAuthUser) (*pb.Profile, error) {
	// TODO: create profile on oauth?
	user, err := store.PutOAuthUser(s.mdb, authinfo)
	if err != nil {
		return nil, err
	}

	// exists user
	if user.Uuid != "" {
		uuid1, err := uuid.FromString(user.Uuid)
		if err != nil {
			return nil, err
		}
		profile, err := store.GetProfileFromUuid(s.mdb, uuid1)
		if err != nil {
			return nil, err
		}

		// build services if profile present
		if authinfo.Provider == "twitter" {
			feedinfo, err := store.GetFeedinfo(s.rdb, profile.Uuid)
			if err != nil {
				return nil, err
			}
			// WARN: goth user.NickName == screen_name which is twitter id
			service := &pb.Service{
				Id:       "twitter",
				Name:     "Twitter",
				Icon:     "/static/images/icons/twitter.png",
				Profile:  "https://twitter.com/" + user.NickName,
				Username: user.Name,
				Oauth:    user,
				Created:  time.Now().Unix(),
				Updated:  time.Now().Unix(),
			}
			feedinfo.Services = append(feedinfo.Services, service)
			err = store.SaveFeedinfo(s.rdb, profile.Uuid, feedinfo)
			if err != nil {
				return nil, err
			}
		}
		return profile, nil
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

func (s *ApiServer) DeleteService(ctx context.Context, req *pb.ServiceRequest) (*pb.Feedinfo, error) {
	feedinfo, err := store.GetFeedinfo(s.rdb, req.User)
	if err != nil {
		return nil, err
	}

	for i, item := range feedinfo.Services {
		if item.Id == req.Service {
			feedinfo.Services = append(feedinfo.Services[:i], feedinfo.Services[i+1:]...)
		}
	}
	if err := store.SaveFeedinfo(s.rdb, feedinfo.Uuid, feedinfo); err != nil {
		return nil, err
	}
	return feedinfo, nil
}
