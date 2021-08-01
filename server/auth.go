package server

import (
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"golang.org/x/net/context"
)

func (s *ApiServer) PutOAuth(ctx context.Context, authinfo *pb.OAuthUser) (*pb.Profile, error) {
	_, msg, err := model.GetOAuthUser(s.mdb, authinfo.Provider, authinfo.UserId)
	if err != nil && err != model.ErrNotFound {
		return nil, err
	}

	// exist oauth
	// WARN: do not gen uuid, old uuid may from ff
	if msg != nil && msg.Uuid != "" {
		authinfo.Uuid = msg.Uuid
	}

	// Update OAuth user or create new OAuth user
	authinfo, err = model.PutOAuthUser(s.mdb, authinfo)
	if err != nil {
		return nil, err
	}
	// logger.Debugf("PutOAuth: <%s, %s:%s>", authinfo.Uuid, "twitter", authinfo.UserId)

	// exists profile
	profileUUid, _ := uuid.FromString(authinfo.Uuid)
	profile, err := model.GetProfileFromUuid(s.mdb, profileUUid)
	if err != nil {
		if err == model.ErrNotFound {
			profile, err = model.NewProfileFromOAuthUser(s.mdb, authinfo)
			if err != nil {
				return nil, err
			}
			logger.Debugf("New profile: <%s>", profile.Uuid)
		} else {
			return nil, err
		}
	}

	// (re)build services if profile present
	if strings.ToLower(authinfo.Provider) == "twitter" {
		// WARN: goth user.NickName == screen_name which is twitter id
		service := &pb.Service{
			Id:       "twitter",
			Name:     "Twitter",
			Icon:     "/static/images/icons/twitter.png",
			Profile:  "https://twitter.com/" + authinfo.NickName,
			Username: authinfo.Name,
			Oauth:    authinfo,
			Created:  time.Now().Unix(),
			Updated:  time.Now().Unix(),
		}

		err = model.PutService(s.rdb, profileUUid, service)
		if err != nil {
			return nil, err
		}
		logger.Debugf("PutService: %s \n %v>", profile.Uuid, service)
	}
	return profile, nil
}

func (s *ApiServer) BindUserFeed(ctx context.Context, user *pb.OAuthUser) (*pb.OAuthUser, error) {
	return model.BindOAuthUser(s.mdb, user)
}

func (s *ApiServer) FetchProfile(ctx context.Context, req *pb.ProfileRequest) (*pb.Profile, error) {
	logger.Debugf("FetchProfile: %s", req.Uuid)
	uuid1, err := uuid.FromString(req.Uuid)
	if err != nil {
		return nil, err
	}
	return model.GetProfileFromUuid(s.mdb, uuid1)
}

func (s *ApiServer) DeleteService(ctx context.Context, req *pb.ServiceRequest) (*pb.Feedinfo, error) {
	logger.Debugf("DeleteService: <%s, %s>", req.User, req.Service)
	uuid1, err := uuid.FromString(req.User)
	if err != nil {
		return nil, err
	}
	model.DeleteService(s.rdb, uuid1, req.Service)
	return &pb.Feedinfo{}, nil
}
