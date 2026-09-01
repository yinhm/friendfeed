package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type importTokenClient struct {
	pb.ApiClient
	revoked bool
}

func (f *importTokenClient) IssueImportOperatorToken(context.Context, *pb.IssueImportOperatorTokenRequest, ...grpc.CallOption) (*pb.ImportOperatorTokenMutationResponse, error) {
	return &pb.ImportOperatorTokenMutationResponse{
		Token: "secret-token", Status: &pb.ImportOperatorTokenStatusResponse{ExpiresAtMs: time.Now().Add(time.Hour).UnixMilli(), Active: true},
	}, nil
}

func (f *importTokenClient) RevokeImportOperatorToken(context.Context, *emptypb.Empty, ...grpc.CallOption) (*pb.ImportOperatorTokenStatusResponse, error) {
	f.revoked = true
	return new(pb.ImportOperatorTokenStatusResponse), nil
}

func TestImportTokenIssueWritesNew0600File(t *testing.T) {
	oldClient, oldTTL, oldOut := apiClient, importTokenTTL, importTokenOut
	t.Cleanup(func() { apiClient, importTokenTTL, importTokenOut = oldClient, oldTTL, oldOut })
	apiClient = &importTokenClient{}
	importTokenTTL = time.Hour
	importTokenOut = filepath.Join(t.TempDir(), "operator-key")
	importTokenIssueCmd.SetContext(context.Background())

	require.NoError(t, importTokenIssueCmd.RunE(importTokenIssueCmd, nil))
	raw, err := os.ReadFile(importTokenOut)
	require.NoError(t, err)
	require.Equal(t, "secret-token\n", string(raw))
	info, err := os.Stat(importTokenOut)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
