package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/protobuf/proto"
)

type oauthIdentityReport struct {
	Provider             string `json:"provider"`
	UserID               string `json:"user_id"`
	Name                 string `json:"name"`
	Nickname             string `json:"nickname"`
	FeedID               string `json:"feed_id"`
	FeedUUID             string `json:"feed_uuid"`
	ProfileIdentityCount int    `json:"profile_identity_count"`
}

func normalizeOAuthIdentity(provider, userID string) (string, string, error) {
	provider, userID = strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(userID)
	if provider == "" || userID == "" {
		return "", "", errors.New("OAuth provider and user ID are required")
	}
	return provider, userID, nil
}

func (s *ApiServer) inspectOAuthIdentity(provider, userID string) (oauthIdentityReport, error) {
	provider, userID, err := normalizeOAuthIdentity(provider, userID)
	if err != nil {
		return oauthIdentityReport{}, err
	}
	_, identity, err := model.GetOAuthUser(s.mdb, provider, userID)
	if err != nil {
		return oauthIdentityReport{}, err
	}
	feedUUID, err := uuid.FromString(identity.Uuid)
	if err != nil || feedUUID == uuid.Nil {
		return oauthIdentityReport{}, errors.New("OAuth identity is not bound to a valid Profile")
	}
	profile, err := model.GetProfileFromUuid(s.mdb, feedUUID)
	if err != nil {
		return oauthIdentityReport{}, err
	}
	count := 0
	err = model.OAuth.Iter(s.mdb, func(_, raw []byte) error {
		candidate := new(pb.OAuthUser)
		if err := proto.Unmarshal(raw, candidate); err != nil {
			return err
		}
		if candidate.Uuid == identity.Uuid {
			count++
		}
		return nil
	})
	if err != nil {
		return oauthIdentityReport{}, err
	}
	return oauthIdentityReport{
		Provider: provider, UserID: userID, Name: identity.Name, Nickname: identity.NickName,
		FeedID: profile.Id, FeedUUID: profile.Uuid, ProfileIdentityCount: count,
	}, nil
}

func (s *ApiServer) oauthMaintenanceResult(provider, userID string, unlink bool) (string, error) {
	if unlink {
		s.profileUpdateMu.Lock()
		defer s.profileUpdateMu.Unlock()
	}
	report, err := s.inspectOAuthIdentity(provider, userID)
	if err != nil {
		return "", err
	}
	if unlink {
		if report.ProfileIdentityCount <= 1 {
			return "", errors.New("refusing to unlink the last OAuth identity from a Profile")
		}
		if err := model.OAuth.Delete(s.mdb, model.KeyFromString(report.Provider, report.UserID)); err != nil {
			return "", fmt.Errorf("delete OAuth identity: %w", err)
		}
		report.ProfileIdentityCount--
		slog.Info("OAuth identity unlinked", "provider", report.Provider, "user_id", report.UserID,
			"feed_uuid", report.FeedUUID, "remaining_identities", report.ProfileIdentityCount)
	}
	raw, err := json.Marshal(report)
	return string(raw), err
}
