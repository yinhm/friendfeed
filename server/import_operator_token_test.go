package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestImportOperatorTokenRPCResolvesCanonicalTargetAndRevokes(t *testing.T) {
	srv := newServiceServer(t)
	profileID := createServiceUser(t, srv, "operator-target")
	profile, err := model.GetProfileFromUuid(srv.rdb, profileID)
	require.NoError(t, err)
	issued, err := srv.IssueImportOperatorToken(context.Background(), &pb.IssueImportOperatorTokenRequest{
		TtlSeconds: int64(time.Hour / time.Second), IssuedBy: "operator@host",
	})
	require.NoError(t, err)
	require.True(t, issued.Status.Active)
	require.Equal(t, "operator@host", issued.Status.IssuedBy)

	principal, err := srv.AuthenticateImportOperatorToken(context.Background(), &pb.AuthenticateImportOperatorTokenRequest{
		Token: issued.Token, TargetFeed: profile.Id,
	})
	require.NoError(t, err)
	require.Equal(t, profile.Uuid, principal.FeedUuid)
	require.Equal(t, issued.Status.KeyId, principal.KeyId)

	_, err = srv.RevokeImportOperatorToken(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	_, err = srv.AuthenticateImportOperatorToken(context.Background(), &pb.AuthenticateImportOperatorTokenRequest{
		Token: issued.Token, TargetFeed: profile.Id,
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}
