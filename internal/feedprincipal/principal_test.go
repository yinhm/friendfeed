package feedprincipal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func TestPrincipalRoundTripCarriesNoSecret(t *testing.T) {
	outgoing, ok := WithOutgoing(context.Background(), "11111111-1111-1111-1111-111111111111", []byte("12345678"))
	require.True(t, ok)
	md, ok := metadata.FromOutgoingContext(outgoing)
	require.True(t, ok)
	incoming := metadata.NewIncomingContext(context.Background(), md)
	principal, ok := FromIncoming(incoming)
	require.True(t, ok)
	require.Equal(t, "11111111-1111-1111-1111-111111111111", principal.FeedUUID.String())
	require.Equal(t, []byte("12345678"), principal.KeyID)
}

func TestPrincipalRejectsPartialOrInvalidMetadata(t *testing.T) {
	_, ok := WithOutgoing(context.Background(), "bad", []byte("12345678"))
	require.False(t, ok)
	for _, values := range []metadata.MD{
		{"x-ff-feed-uuid": {"11111111-1111-1111-1111-111111111111"}},
		{"x-ff-feed-uuid": {"11111111-1111-1111-1111-111111111111"}, "x-ff-feed-key-id": {"bad"}},
	} {
		_, ok := FromIncoming(metadata.NewIncomingContext(context.Background(), values))
		require.False(t, ok)
	}
}
