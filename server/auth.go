package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func (s *ApiServer) PutOAuth(ctx context.Context, authinfo *pb.OAuthUser) (*pb.Profile, error) {
	slog.Debug("auth info", "provider", authinfo.Provider, "user_id", authinfo.UserId, "uuid", authinfo.Uuid)
	_, msg, err := model.GetOAuthUser(s.mdb, authinfo.Provider, authinfo.UserId)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		slog.Debug("oauth user not found", "err", err)
		return nil, err
	}
	if msg != nil {
		slog.Debug("oauth user found", "provider", msg.Provider, "user_id", msg.UserId, "uuid", msg.Uuid)
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
	slog.Debug("PutOAuth", "uuid", authinfo.Uuid, "provider", authinfo.Provider, "user_id", authinfo.UserId)

	// exists profile
	profileUUID, _ := uuid.FromString(authinfo.Uuid)
	profile, err := model.GetProfileFromUuid(s.mdb, profileUUID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			profile, err = model.NewProfileFromOAuthUser(s.mdb, authinfo)
			if err != nil {
				return nil, err
			}
			// Transient response flag, set after the profile write so it is
			// never persisted: lets the web layer redirect first-time users
			// to the profile page to pick their own ID.
			profile.NewlyCreated = true
			slog.Debug("New profile", "uuid", profile.Uuid)
		} else {
			return nil, err
		}
	}

	// (re)build services if profile present
	if strings.ToLower(authinfo.Provider) == "twitter" {
		// WARN: goth user.NickName == screen_name which is twitter id
		service := &pb.FeedService{
			Id:       "twitter",
			Name:     "Twitter",
			Icon:     "/static/images/icons/twitter.png",
			Profile:  "https://twitter.com/" + authinfo.NickName,
			Username: authinfo.Name,
			Oauth:    authinfo,
			Created:  time.Now().Unix(),
			Updated:  time.Now().Unix(),
		}

		err = model.PutFeedService(s.rdb, profileUUID, service)
		if err != nil {
			return nil, err
		}
		slog.Debug("PutFeedService", "uuid", profile.Uuid, "username", service.Username)
	}
	// Login must not wait for a potentially expensive Home rebuild. Prewarm in
	// the same bounded, per-viewer background path used by FetchFeed.
	s.scheduleHomeTimelineMaintenance(profileUUID, time.Now().UTC())
	return profile, nil
}

func (s *ApiServer) BindUserFeed(ctx context.Context, user *pb.OAuthUser) (*pb.OAuthUser, error) {
	return model.BindOAuthUser(s.mdb, user)
}

func (s *ApiServer) FetchProfile(ctx context.Context, req *pb.ProfileRequest) (*pb.Profile, error) {
	slog.Debug("FetchProfile", "uuid", req.Uuid, "id", req.Id)
	if req.Uuid == "" && req.Id != "" {
		profile, err := model.GetProfileFromUserId(s.mdb, req.Id)
		if err != nil {
			slog.Debug("FetchProfile", "id", req.Id, "err", err)
			return nil, err
		}
		return profile, nil
	}
	profileUUID, err := uuid.FromString(req.Uuid)
	if err != nil {
		return nil, err
	}
	profile, err := model.GetProfileFromUuid(s.mdb, profileUUID)
	if err != nil {
		slog.Debug("FetchProfile", "err", err)
		return nil, err
	}
	return profile, nil
}
