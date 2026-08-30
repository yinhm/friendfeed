package model

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/store"
)

func openFeedApiKeyTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db
}

func TestFeedApiTokenParserRejectsInvalidInputs(t *testing.T) {
	feed := uuid.Must(uuid.NewV4())
	valid := encodeFeedApiToken(feed, bytes.Repeat([]byte{1}, feedApiKeyIDSize), bytes.Repeat([]byte{2}, feedApiSecretSize))
	parsed, err := parseFeedApiToken(valid)
	require.NoError(t, err)
	require.Equal(t, feed, parsed.feed)

	zero := encodeFeedApiToken(uuid.Nil, bytes.Repeat([]byte{1}, feedApiKeyIDSize), bytes.Repeat([]byte{2}, feedApiSecretSize))
	for _, token := range []string{
		"", "ffk2_a_b_c", "ffk1_only_two", zero,
		strings.Replace(valid, "ffk1_", "ffk1_!", 1),
		encodeFeedApiToken(feed, []byte{1}, bytes.Repeat([]byte{2}, feedApiSecretSize)),
		encodeFeedApiToken(feed, bytes.Repeat([]byte{1}, feedApiKeyIDSize), []byte{2}),
	} {
		_, err := parseFeedApiToken(token)
		require.ErrorIs(t, err, ErrInvalidFeedApiKey, token)
	}
}

func TestFeedApiKeyLifecycle(t *testing.T) {
	db := openFeedApiKeyTestStore(t)
	feed := uuid.Must(uuid.NewV4())
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

	record, token, err := GenerateFeedApiKey(db, feed, now)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Len(t, record.KeyId, feedApiKeyIDSize)
	require.Len(t, record.SecretSha256, 32)
	require.Equal(t, now.UnixMilli(), record.CreatedAtMs)

	authFeed, keyID, err := AuthenticateFeedApiKey(db, token)
	require.NoError(t, err)
	require.Equal(t, feed, authFeed)
	require.Equal(t, record.KeyId, keyID)
	keyID[0] ^= 0xff
	stored, err := GetFeedApiKey(db, feed)
	require.NoError(t, err)
	require.NotEqual(t, keyID, stored.KeyId, "returned key IDs must not alias persisted metadata")

	_, _, err = GenerateFeedApiKey(db, feed, now.Add(time.Second))
	require.ErrorIs(t, err, ErrFeedApiKeyExists)

	rotated, rotatedToken, err := RotateFeedApiKey(db, feed, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotEqual(t, token, rotatedToken)
	require.Equal(t, record.CreatedAtMs, rotated.CreatedAtMs)
	require.Equal(t, now.Add(time.Minute).UnixMilli(), rotated.RotatedAtMs)
	_, _, err = AuthenticateFeedApiKey(db, token)
	require.ErrorIs(t, err, ErrInvalidFeedApiKey)
	_, _, err = AuthenticateFeedApiKey(db, rotatedToken)
	require.NoError(t, err)

	revoked, err := RevokeFeedApiKey(db, feed, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.Empty(t, revoked.SecretSha256)
	require.Equal(t, now.Add(2*time.Minute).UnixMilli(), revoked.RevokedAtMs)
	_, _, err = AuthenticateFeedApiKey(db, rotatedToken)
	require.ErrorIs(t, err, ErrInvalidFeedApiKey)

	again, err := RevokeFeedApiKey(db, feed, now.Add(3*time.Minute))
	require.NoError(t, err)
	require.Equal(t, revoked.RevokedAtMs, again.RevokedAtMs, "revoke must be idempotent")
	_, _, err = RotateFeedApiKey(db, feed, now.Add(4*time.Minute))
	require.ErrorIs(t, err, ErrFeedApiKeyInactive)

	regenerated, regeneratedToken, err := GenerateFeedApiKey(db, feed, now.Add(5*time.Minute))
	require.NoError(t, err)
	require.Equal(t, now.Add(5*time.Minute).UnixMilli(), regenerated.CreatedAtMs)
	require.NotEqual(t, rotatedToken, regeneratedToken)
}

func TestConcurrentGenerateFeedApiKeyHasSingleWinner(t *testing.T) {
	db := openFeedApiKeyTestStore(t)
	feed := uuid.Must(uuid.NewV4())
	const racers = 12
	var wg sync.WaitGroup
	errs := make(chan error, racers)
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := GenerateFeedApiKey(db, feed, time.Now())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	winners := 0
	for err := range errs {
		if err == nil {
			winners++
			continue
		}
		require.True(t, errors.Is(err, ErrFeedApiKeyExists), err)
	}
	require.Equal(t, 1, winners)
}

func TestFeedApiKeyPersistsWithoutExplicitFlushAndDoesNotStoreToken(t *testing.T) {
	dir := t.TempDir()
	db, err := store.NewStore(dir)
	require.NoError(t, err)
	feed := uuid.Must(uuid.NewV4())
	_, token, err := GenerateFeedApiKey(db, feed, time.Now())
	require.NoError(t, err)
	raw, err := db.Get(FeedApiKey.PrefixAppend(feed.Bytes()))
	require.NoError(t, err)
	require.NotContains(t, string(raw), token)
	db.Close()

	db, err = store.NewStore(dir)
	require.NoError(t, err)
	defer db.Close()
	got, _, err := AuthenticateFeedApiKey(db, token)
	require.NoError(t, err)
	require.Equal(t, feed, got)
}
