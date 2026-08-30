package server

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func parseFeedApiKeyManageRequest(request *pb.FeedApiKeyManageRequest) (uuid.UUID, uuid.UUID, error) {
	if request == nil {
		return uuid.Nil, uuid.Nil, status.Error(codes.InvalidArgument, "request is required")
	}
	actor, actorErr := uuid.FromString(request.ActorUuid)
	feed, feedErr := uuid.FromString(request.FeedUuid)
	if actorErr != nil || feedErr != nil || actor == uuid.Nil || feed == uuid.Nil {
		return uuid.Nil, uuid.Nil, status.Error(codes.InvalidArgument, "valid actor_uuid and feed_uuid are required")
	}
	return actor, feed, nil
}

func feedApiKeyStatus(feed uuid.UUID, record *pb.FeedApiKeyRecord) *pb.FeedApiKeyStatusResponse {
	if record == nil {
		return &pb.FeedApiKeyStatusResponse{FeedUuid: feed.String()}
	}
	return &pb.FeedApiKeyStatusResponse{
		FeedUuid:    feed.String(),
		KeyId:       append([]byte(nil), record.KeyId...),
		CreatedAtMs: record.CreatedAtMs,
		RotatedAtMs: record.RotatedAtMs,
		RevokedAtMs: record.RevokedAtMs,
		Active:      record.RevokedAtMs == 0 && len(record.SecretSha256) != 0,
	}
}

func feedApiKeyModelError(err error) error {
	switch {
	case errors.Is(err, model.ErrFeedApiKeyExists):
		return status.Error(codes.AlreadyExists, "Feed API key already active")
	case errors.Is(err, model.ErrFeedApiKeyInactive):
		return status.Error(codes.FailedPrecondition, "Feed API key is not active")
	case errors.Is(err, model.ErrNotFound):
		return status.Error(codes.NotFound, "Feed API key not found")
	default:
		return status.Error(codes.Internal, "Feed API key operation failed")
	}
}

func (s *ApiServer) authorizeFeedApiKeyManage(actor, feed uuid.UUID) error {
	if err := s.authorizeFeedServiceAdmin(actor, feed); err != nil {
		return status.Error(codes.PermissionDenied, "actor may not manage this Feed API key")
	}
	return nil
}

func logFeedApiKeyManagement(action, result string, actor, feed uuid.UUID, keyID []byte) {
	slog.Info("feed_api_key_management",
		"action", action,
		"result", result,
		"actor_uuid", actor.String(),
		"feed_uuid", feed.String(),
		"key_id", base64.RawURLEncoding.EncodeToString(keyID),
	)
}

func (s *ApiServer) GetFeedApiKeyStatus(_ context.Context, request *pb.FeedApiKeyManageRequest) (*pb.FeedApiKeyStatusResponse, error) {
	actor, feed, err := parseFeedApiKeyManageRequest(request)
	if err != nil {
		return nil, err
	}
	s.profileUpdateMu.Lock()
	defer s.profileUpdateMu.Unlock()
	if err := s.authorizeFeedApiKeyManage(actor, feed); err != nil {
		return nil, err
	}
	record, err := model.GetFeedApiKey(s.rdb, feed)
	if errors.Is(err, model.ErrNotFound) {
		return feedApiKeyStatus(feed, nil), nil
	}
	if err != nil {
		return nil, feedApiKeyModelError(err)
	}
	return feedApiKeyStatus(feed, record), nil
}

func (s *ApiServer) GenerateFeedApiKey(_ context.Context, request *pb.FeedApiKeyManageRequest) (*pb.FeedApiKeyMutationResponse, error) {
	actor, feed, err := parseFeedApiKeyManageRequest(request)
	if err != nil {
		return nil, err
	}
	s.profileUpdateMu.Lock()
	defer s.profileUpdateMu.Unlock()
	if err := s.authorizeFeedApiKeyManage(actor, feed); err != nil {
		logFeedApiKeyManagement("generate", "denied", actor, feed, nil)
		return nil, err
	}
	record, token, err := model.GenerateFeedApiKey(s.rdb, feed, time.Now().UTC())
	if err != nil {
		logFeedApiKeyManagement("generate", "failed", actor, feed, nil)
		return nil, feedApiKeyModelError(err)
	}
	logFeedApiKeyManagement("generate", "ok", actor, feed, record.KeyId)
	return &pb.FeedApiKeyMutationResponse{Status: feedApiKeyStatus(feed, record), Token: token}, nil
}

func (s *ApiServer) RotateFeedApiKey(_ context.Context, request *pb.FeedApiKeyManageRequest) (*pb.FeedApiKeyMutationResponse, error) {
	actor, feed, err := parseFeedApiKeyManageRequest(request)
	if err != nil {
		return nil, err
	}
	s.profileUpdateMu.Lock()
	defer s.profileUpdateMu.Unlock()
	if err := s.authorizeFeedApiKeyManage(actor, feed); err != nil {
		logFeedApiKeyManagement("rotate", "denied", actor, feed, nil)
		return nil, err
	}
	record, token, err := model.RotateFeedApiKey(s.rdb, feed, time.Now().UTC())
	if err != nil {
		logFeedApiKeyManagement("rotate", "failed", actor, feed, nil)
		return nil, feedApiKeyModelError(err)
	}
	logFeedApiKeyManagement("rotate", "ok", actor, feed, record.KeyId)
	return &pb.FeedApiKeyMutationResponse{Status: feedApiKeyStatus(feed, record), Token: token}, nil
}

func (s *ApiServer) RevokeFeedApiKey(_ context.Context, request *pb.FeedApiKeyManageRequest) (*pb.FeedApiKeyStatusResponse, error) {
	actor, feed, err := parseFeedApiKeyManageRequest(request)
	if err != nil {
		return nil, err
	}
	s.profileUpdateMu.Lock()
	defer s.profileUpdateMu.Unlock()
	if err := s.authorizeFeedApiKeyManage(actor, feed); err != nil {
		logFeedApiKeyManagement("revoke", "denied", actor, feed, nil)
		return nil, err
	}
	record, err := model.RevokeFeedApiKey(s.rdb, feed, time.Now().UTC())
	if err != nil {
		logFeedApiKeyManagement("revoke", "failed", actor, feed, nil)
		return nil, feedApiKeyModelError(err)
	}
	logFeedApiKeyManagement("revoke", "ok", actor, feed, record.KeyId)
	return feedApiKeyStatus(feed, record), nil
}

func (s *ApiServer) AuthenticateFeedApiKey(_ context.Context, request *pb.AuthenticateFeedApiKeyRequest) (*pb.AuthenticateFeedApiKeyResponse, error) {
	if request == nil || request.Token == "" {
		return nil, status.Error(codes.Unauthenticated, "invalid Feed API key")
	}
	feed, keyID, err := model.AuthenticateFeedApiKey(s.rdb, request.Token)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid Feed API key")
	}
	if _, err := model.GetProfileFromUuid(s.rdb, feed); err != nil {
		return nil, status.Error(codes.FailedPrecondition, "Feed is unavailable")
	}
	return &pb.AuthenticateFeedApiKeyResponse{FeedUuid: feed.String(), KeyId: keyID}, nil
}
