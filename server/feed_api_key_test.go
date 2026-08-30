package server

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func manageFeedApiKeyRequest(actor, feed uuid.UUID) *pb.FeedApiKeyManageRequest {
	return &pb.FeedApiKeyManageRequest{ActorUuid: actor.String(), FeedUuid: feed.String()}
}

func TestPersonalFeedApiKeyLifecycleRPC(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "api-owner")
	request := manageFeedApiKeyRequest(owner, owner)

	statusResponse, err := srv.GetFeedApiKeyStatus(context.Background(), request)
	require.NoError(t, err)
	require.False(t, statusResponse.Active)
	require.Empty(t, statusResponse.KeyId)

	generated, err := srv.GenerateFeedApiKey(context.Background(), request)
	require.NoError(t, err)
	require.NotEmpty(t, generated.Token)
	require.True(t, generated.Status.Active)
	require.NotEmpty(t, generated.Status.KeyId)

	statusResponse, err = srv.GetFeedApiKeyStatus(context.Background(), request)
	require.NoError(t, err)
	require.True(t, statusResponse.Active)
	require.Equal(t, generated.Status.KeyId, statusResponse.KeyId)

	authenticated, err := srv.AuthenticateFeedApiKey(context.Background(), &pb.AuthenticateFeedApiKeyRequest{Token: generated.Token})
	require.NoError(t, err)
	require.Equal(t, owner.String(), authenticated.FeedUuid)
	require.Equal(t, generated.Status.KeyId, authenticated.KeyId)

	_, err = srv.GenerateFeedApiKey(context.Background(), request)
	require.Equal(t, codes.AlreadyExists, status.Code(err))

	rotated, err := srv.RotateFeedApiKey(context.Background(), request)
	require.NoError(t, err)
	require.NotEqual(t, generated.Token, rotated.Token)
	_, err = srv.AuthenticateFeedApiKey(context.Background(), &pb.AuthenticateFeedApiKeyRequest{Token: generated.Token})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	revoked, err := srv.RevokeFeedApiKey(context.Background(), request)
	require.NoError(t, err)
	require.False(t, revoked.Active)
	require.NotEmpty(t, revoked.GetKeyId())
	_, err = srv.AuthenticateFeedApiKey(context.Background(), &pb.AuthenticateFeedApiKeyRequest{Token: rotated.Token})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	again, err := srv.RevokeFeedApiKey(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, revoked.RevokedAtMs, again.RevokedAtMs)
}

func TestFeedApiKeyManagementAuthorization(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "key-owner")
	outsider := createServiceUser(t, srv, "key-outsider")
	_, err := srv.GenerateFeedApiKey(context.Background(), manageFeedApiKeyRequest(outsider, owner))
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	superID := createServiceUser(t, srv, "key-super")
	superProfile, err := model.GetProfileFromUuid(srv.rdb, superID)
	require.NoError(t, err)
	superProfile.IsSuper = true
	require.NoError(t, model.UpdateProfile(srv.rdb, superProfile))
	_, err = srv.GenerateFeedApiKey(context.Background(), manageFeedApiKeyRequest(superID, owner))
	require.NoError(t, err)
}

func TestGroupFeedApiKeyRequiresAdmin(t *testing.T) {
	srv := newServiceServer(t)
	admin := createServiceUser(t, srv, "group-key-admin")
	member := createServiceUser(t, srv, "group-key-member")
	group := createTestGroup(t, srv, admin, "group-key")
	require.NoError(t, model.JoinGroup(srv.rdb, group, member))

	_, err := srv.GenerateFeedApiKey(context.Background(), manageFeedApiKeyRequest(member, group))
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	generated, err := srv.GenerateFeedApiKey(context.Background(), manageFeedApiKeyRequest(admin, group))
	require.NoError(t, err)
	require.NotEmpty(t, generated.Token)
}

func TestAuthenticateFeedApiKeyUsesUniformFailureAndRejectsDeletedFeed(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "deleted-key-owner")
	generated, err := srv.GenerateFeedApiKey(context.Background(), manageFeedApiKeyRequest(owner, owner))
	require.NoError(t, err)

	for _, token := range []string{"", "malformed", generated.Token + "x"} {
		_, err := srv.AuthenticateFeedApiKey(context.Background(), &pb.AuthenticateFeedApiKeyRequest{Token: token})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
		require.Equal(t, "invalid Feed API key", status.Convert(err).Message())
	}

	profile, err := model.GetProfileFromUuid(srv.rdb, owner)
	require.NoError(t, err)
	profile.Deleted = true
	_, err = model.Profile.Put(srv.rdb, owner.Bytes(), profile)
	require.NoError(t, err)
	_, err = srv.AuthenticateFeedApiKey(context.Background(), &pb.AuthenticateFeedApiKeyRequest{Token: generated.Token})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}
