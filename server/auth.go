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
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
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

	profileUUID, _ := uuid.FromString(authinfo.Uuid)

	// (re)build services BEFORE the profile exists. A service-write failure
	// must not strand a half-created account: when this fails no profile has
	// been written yet, so the next login still counts as first-time and
	// keeps onboarding. The service key is the deterministic profile UUID,
	// so a row written here is adopted by the profile the next login creates
	// instead of being orphaned.
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

		if err := model.PutFeedService(s.rdb, profileUUID, service); err != nil {
			return nil, err
		}
		slog.Debug("PutFeedService", "uuid", authinfo.Uuid, "username", service.Username)
	}

	// The get-or-create commits atomically: concurrent first logins of the
	// same account cannot mint two profiles or two ID aliases for one UUID.
	profile, created, err := model.GetOrCreateProfileFromOAuthUser(s.mdb, authinfo)
	if err != nil {
		return nil, err
	}
	if created {
		// Signal first-time login via response header metadata, not a
		// Profile field: Profile is a persisted type and must not carry
		// transient RPC state. Outside a gRPC server context (direct
		// calls in tests) there is no transport, so ignore the error.
		if err := grpc.SetHeader(ctx, metadata.Pairs(pb.ProfileNewlyCreatedHeader, "true")); err != nil {
			slog.Debug("SetHeader newly-created", "err", err)
		}
		slog.Debug("New profile", "uuid", profile.Uuid)
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
