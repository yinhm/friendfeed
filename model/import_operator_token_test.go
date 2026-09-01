package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestImportOperatorTokenLifecycle(t *testing.T) {
	db := openFeedApiKeyTestStore(t)
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

	record, token, err := IssueImportOperatorToken(db, now, time.Hour, "operator@host")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Equal(t, now.Add(time.Hour).UnixMilli(), record.ExpiresAtMs)
	keyID, err := AuthenticateImportOperatorToken(db, token, now.Add(59*time.Minute))
	require.NoError(t, err)
	require.Equal(t, record.KeyId, keyID)
	_, err = AuthenticateImportOperatorToken(db, token, now.Add(time.Hour))
	require.ErrorIs(t, err, ErrInvalidOperatorToken)

	_, replacement, err := IssueImportOperatorToken(db, now.Add(time.Minute), time.Minute, "operator@host")
	require.NoError(t, err)
	_, err = AuthenticateImportOperatorToken(db, token, now.Add(time.Minute))
	require.ErrorIs(t, err, ErrInvalidOperatorToken)
	_, err = RevokeImportOperatorToken(db, now.Add(2*time.Minute))
	require.NoError(t, err)
	_, err = AuthenticateImportOperatorToken(db, replacement, now.Add(2*time.Minute))
	require.ErrorIs(t, err, ErrInvalidOperatorToken)
}

func TestImportOperatorTokenRejectsInvalidTTLAndNeverPersistsPlaintext(t *testing.T) {
	db := openFeedApiKeyTestStore(t)
	_, _, err := IssueImportOperatorToken(db, time.Now(), time.Hour+time.Second, "operator@host")
	require.ErrorIs(t, err, ErrInvalidOperatorTTL)

	_, token, err := IssueImportOperatorToken(db, time.Now(), time.Hour, "operator@host")
	require.NoError(t, err)
	raw, err := db.Get(ImportOperatorTokenMetaKey())
	require.NoError(t, err)
	require.NotContains(t, string(raw), token)
}
