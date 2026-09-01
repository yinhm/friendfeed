package server

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func importOperatorTokenStatus(record *pb.ImportOperatorTokenRecord, now time.Time) *pb.ImportOperatorTokenStatusResponse {
	if record == nil {
		return new(pb.ImportOperatorTokenStatusResponse)
	}
	return &pb.ImportOperatorTokenStatusResponse{
		KeyId: append([]byte(nil), record.KeyId...), CreatedAtMs: record.CreatedAtMs,
		ExpiresAtMs: record.ExpiresAtMs, RevokedAtMs: record.RevokedAtMs,
		IssuedBy: record.IssuedBy,
		Active:   record.RevokedAtMs == 0 && len(record.SecretSha256) != 0 && now.UTC().UnixMilli() < record.ExpiresAtMs,
	}
}

func logImportOperatorToken(action, result string, keyID []byte) {
	slog.Info("import_operator_token_management", "action", action, "result", result,
		"key_id", base64.RawURLEncoding.EncodeToString(keyID))
}

func (s *ApiServer) GetImportOperatorTokenStatus(_ context.Context, _ *emptypb.Empty) (*pb.ImportOperatorTokenStatusResponse, error) {
	record, err := model.GetImportOperatorToken(s.rdb)
	if errors.Is(err, store.ErrNotFound) {
		return importOperatorTokenStatus(nil, time.Now()), nil
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "inspect import operator token failed")
	}
	return importOperatorTokenStatus(record, time.Now()), nil
}

func (s *ApiServer) IssueImportOperatorToken(_ context.Context, request *pb.IssueImportOperatorTokenRequest) (*pb.ImportOperatorTokenMutationResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if request.TtlSeconds < 1 || request.TtlSeconds > int64(model.MaxImportOperatorTokenTTL/time.Second) {
		return nil, status.Error(codes.InvalidArgument, model.ErrInvalidOperatorTTL.Error())
	}
	issuedBy := strings.TrimSpace(request.IssuedBy)
	if issuedBy == "" || len(issuedBy) > 128 || strings.IndexFunc(issuedBy, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._@:/-", r))
	}) >= 0 {
		return nil, status.Error(codes.InvalidArgument, "issued_by is invalid")
	}
	ttl := time.Duration(request.TtlSeconds) * time.Second
	record, token, err := model.IssueImportOperatorToken(s.rdb, time.Now().UTC(), ttl, issuedBy)
	if errors.Is(err, model.ErrInvalidOperatorTTL) {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err != nil {
		logImportOperatorToken("issue", "failed", nil)
		return nil, status.Error(codes.Internal, "issue import operator token failed")
	}
	logImportOperatorToken("issue", "ok", record.KeyId)
	return &pb.ImportOperatorTokenMutationResponse{Status: importOperatorTokenStatus(record, time.Now()), Token: token}, nil
}

func (s *ApiServer) RevokeImportOperatorToken(_ context.Context, _ *emptypb.Empty) (*pb.ImportOperatorTokenStatusResponse, error) {
	record, err := model.RevokeImportOperatorToken(s.rdb, time.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		return importOperatorTokenStatus(nil, time.Now()), nil
	}
	if err != nil {
		logImportOperatorToken("revoke", "failed", nil)
		return nil, status.Error(codes.Internal, "revoke import operator token failed")
	}
	logImportOperatorToken("revoke", "ok", record.KeyId)
	return importOperatorTokenStatus(record, time.Now()), nil
}

func (s *ApiServer) AuthenticateImportOperatorToken(_ context.Context, request *pb.AuthenticateImportOperatorTokenRequest) (*pb.AuthenticateFeedApiKeyResponse, error) {
	if request == nil || request.Token == "" || request.TargetFeed == "" {
		return nil, status.Error(codes.Unauthenticated, "invalid import operator token")
	}
	keyID, err := model.AuthenticateImportOperatorToken(s.rdb, request.Token, time.Now().UTC())
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid import operator token")
	}
	profile, feed, err := storedProfileByIdentifier(s.rdb, request.TargetFeed)
	if err != nil || profile.Deleted {
		return nil, status.Error(codes.FailedPrecondition, "Feed is unavailable")
	}
	return &pb.AuthenticateFeedApiKeyResponse{FeedUuid: feed.String(), KeyId: keyID}, nil
}
