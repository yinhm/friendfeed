// Package feedprincipal carries the already-authenticated Feed capability
// across the loopback ffweb -> ffdb gRPC boundary. It never carries secrets.
package feedprincipal

import (
	"context"
	"encoding/base64"

	"github.com/gofrs/uuid"
	"google.golang.org/grpc/metadata"
)

const (
	feedMetadataKey  = "x-ff-feed-uuid"
	keyIDMetadataKey = "x-ff-feed-key-id"
	keyIDSize        = 8
)

type Principal struct {
	FeedUUID uuid.UUID
	KeyID    []byte
}

func WithOutgoing(ctx context.Context, feedRaw string, keyID []byte) (context.Context, bool) {
	feed, err := uuid.FromString(feedRaw)
	if err != nil || feed == uuid.Nil || len(keyID) != keyIDSize {
		return ctx, false
	}
	return metadata.AppendToOutgoingContext(ctx,
		feedMetadataKey, feed.String(),
		keyIDMetadataKey, base64.RawURLEncoding.EncodeToString(keyID),
	), true
}

func FromIncoming(ctx context.Context) (Principal, bool) {
	values, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return Principal{}, false
	}
	feeds, keys := values.Get(feedMetadataKey), values.Get(keyIDMetadataKey)
	if len(feeds) != 1 || len(keys) != 1 {
		return Principal{}, false
	}
	feed, err := uuid.FromString(feeds[0])
	if err != nil || feed == uuid.Nil {
		return Principal{}, false
	}
	keyID, err := base64.RawURLEncoding.DecodeString(keys[0])
	if err != nil || len(keyID) != keyIDSize {
		return Principal{}, false
	}
	return Principal{FeedUUID: feed, KeyID: append([]byte(nil), keyID...)}, true
}
