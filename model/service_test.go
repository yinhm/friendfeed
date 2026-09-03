package model

import (
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func TestNormalizeServiceURLPreservesQueryIdentity(t *testing.T) {
	got, err := NormalizeServiceURL(" HTTPS://Example.COM:443/feed?b=2&a=1#fragment ")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/feed?b=2&a=1", got)
	_, err = NormalizeServiceURL("file:///tmp/feed")
	require.Error(t, err)
	_, err = NormalizeServiceURL("https://user:secret@example.com/feed")
	require.Error(t, err)
}

func TestServiceIdentityIncludesKind(t *testing.T) {
	normalized, first, err := ServiceIdentity(WebFeedServiceKind, "https://example.com/feed")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/feed", normalized)
	_, second, err := ServiceIdentity(WebFeedServiceKind, normalized)
	require.NoError(t, err)
	require.Equal(t, first, second)
	_, _, err = ServiceIdentity("twitter", normalized)
	require.Error(t, err)
}

func TestServiceAndFeedServiceKeys(t *testing.T) {
	service := uuid.Must(uuid.NewV4())
	target := uuid.Must(uuid.NewV4())
	feedKey, err := FeedServiceKey(target, "rss")
	require.NoError(t, err)
	require.Equal(t, append(append(KeyPrefixToBytes(TableFeedService), target.Bytes()...), []byte("rss")...), []byte(feedKey))
	indexKey, err := ServiceFeedIndexKey(service, target, "rss")
	require.NoError(t, err)
	require.Equal(t, append(append(append(KeyPrefixToBytes(TableServiceFeedIndex), service.Bytes()...), target.Bytes()...), []byte("rss")...), []byte(indexKey))
}

func TestStageAddBuiltinBingWallpaperService(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	target := uuid.Must(uuid.NewV4())
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	var binding *pb.FeedService
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		var err error
		binding, _, err = StageAddBuiltinFeedService(db, batch, target, BingWallpaperServiceKind, now)
		return err
	}))
	require.Equal(t, BingWallpaperServiceKind, binding.Kind)
	require.Empty(t, binding.AddedByUuid)
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	service, err := GetService(db, serviceID)
	require.NoError(t, err)
	require.Equal(t, BingWallpaperCanonicalURL, service.CanonicalUrl)
	require.Equal(t, BingWallpaperFetchURL, service.FetchUrl)

	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		repeated, _, err := StageAddBuiltinFeedService(db, batch, target, BingWallpaperServiceKind, now.Add(time.Hour))
		require.Equal(t, binding.ServiceUuid, repeated.ServiceUuid)
		return err
	}))
}

func TestExistingFeedServiceEncodingRemainsReadable(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	target := uuid.Must(uuid.NewV4())
	legacy := &pb.FeedService{Id: "twitter", Name: "Twitter", Username: "user", Oauth: &pb.OAuthUser{Provider: "twitter"}}
	require.NoError(t, PutFeedService(db, target, legacy))
	got, err := GetFeedService(db, target, "twitter")
	require.NoError(t, err)
	require.True(t, proto.Equal(legacy, got))
}

func TestServiceStateRejectsMismatchedIdentity(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	require.Error(t, PutServiceState(db, uuid.Nil, &pb.ServiceState{}))
	id := uuid.Must(uuid.NewV4())
	require.Error(t, PutServiceState(db, id, &pb.ServiceState{ServiceUuid: uuid.Must(uuid.NewV4()).String()}))
}
