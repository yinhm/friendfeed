package model

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

const (
	feedApiTokenVersion = "ffk1"
	feedApiKeyIDSize    = 8
	feedApiSecretSize   = 32
)

var (
	ErrFeedApiKeyExists   = errors.New("feed API key already active")
	ErrFeedApiKeyInactive = errors.New("feed API key is not active")
	ErrInvalidFeedApiKey  = errors.New("invalid feed API key")
)

type parsedFeedApiToken struct {
	feed   uuid.UUID
	keyID  []byte
	secret []byte
}

func encodeFeedApiToken(feed uuid.UUID, keyID, secret []byte) string {
	enc := base64.RawURLEncoding
	return strings.Join([]string{feedApiTokenVersion, enc.EncodeToString(feed.Bytes()), enc.EncodeToString(keyID), enc.EncodeToString(secret)}, "_")
}

func parseFeedApiToken(token string) (parsedFeedApiToken, error) {
	// Raw Base64URL itself may contain underscores, so separators cannot be
	// found with strings.Split. All three payloads have fixed byte lengths and
	// therefore fixed unpadded encoded lengths; slice by those boundaries.
	const (
		prefixLen = len(feedApiTokenVersion) + 1
		feedLen   = 22 // RawURLEncoding.EncodedLen(16)
		keyIDLen  = 11 // RawURLEncoding.EncodedLen(8)
		secretLen = 43 // RawURLEncoding.EncodedLen(32)
		totalLen  = prefixLen + feedLen + 1 + keyIDLen + 1 + secretLen
	)
	if len(token) != totalLen || !strings.HasPrefix(token, feedApiTokenVersion+"_") {
		return parsedFeedApiToken{}, ErrInvalidFeedApiKey
	}
	feedEnd := prefixLen + feedLen
	keyIDStart := feedEnd + 1
	keyIDEnd := keyIDStart + keyIDLen
	secretStart := keyIDEnd + 1
	if token[feedEnd] != '_' || token[keyIDEnd] != '_' {
		return parsedFeedApiToken{}, ErrInvalidFeedApiKey
	}
	parts := [3]string{token[prefixLen:feedEnd], token[keyIDStart:keyIDEnd], token[secretStart:]}
	decode := func(raw string, size int) ([]byte, error) {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(decoded) != size {
			return nil, ErrInvalidFeedApiKey
		}
		return decoded, nil
	}
	feedRaw, err := decode(parts[0], uuid.Size)
	if err != nil {
		return parsedFeedApiToken{}, err
	}
	feed, err := uuid.FromBytes(feedRaw)
	if err != nil || feed == uuid.Nil {
		return parsedFeedApiToken{}, ErrInvalidFeedApiKey
	}
	keyID, err := decode(parts[1], feedApiKeyIDSize)
	if err != nil {
		return parsedFeedApiToken{}, err
	}
	secret, err := decode(parts[2], feedApiSecretSize)
	if err != nil {
		return parsedFeedApiToken{}, err
	}
	return parsedFeedApiToken{feed: feed, keyID: keyID, secret: secret}, nil
}

func newFeedApiCredential(feed uuid.UUID) (*pb.FeedApiKeyRecord, string, error) {
	keyID := make([]byte, feedApiKeyIDSize)
	secret := make([]byte, feedApiSecretSize)
	if _, err := rand.Read(keyID); err != nil {
		return nil, "", fmt.Errorf("generate Feed API key ID: %w", err)
	}
	if _, err := rand.Read(secret); err != nil {
		return nil, "", fmt.Errorf("generate Feed API secret: %w", err)
	}
	digest := sha256.Sum256(secret)
	return &pb.FeedApiKeyRecord{KeyId: keyID, SecretSha256: digest[:]}, encodeFeedApiToken(feed, keyID, secret), nil
}

func readFeedApiKey(db *store.Store, feed uuid.UUID) (*pb.FeedApiKeyRecord, error) {
	record := new(pb.FeedApiKeyRecord)
	if err := FeedApiKey.Get(db, feed.Bytes(), record); err != nil {
		return nil, err
	}
	return record, nil
}

func stageFeedApiKey(batch *pebble.Batch, feed uuid.UUID, record *pb.FeedApiKeyRecord) error {
	encoded, err := proto.Marshal(record)
	if err != nil {
		return err
	}
	return batch.Set(FeedApiKey.PrefixAppend(feed.Bytes()), encoded, nil)
}

// GetFeedApiKey returns a defensive copy of persisted metadata. Plaintext is
// never available here because it is not stored.
func GetFeedApiKey(db *store.Store, feed uuid.UUID) (*pb.FeedApiKeyRecord, error) {
	if feed == uuid.Nil {
		return nil, ErrInvalidFeedApiKey
	}
	record, err := readFeedApiKey(db, feed)
	if err != nil {
		return nil, err
	}
	return proto.Clone(record).(*pb.FeedApiKeyRecord), nil
}

func GenerateFeedApiKey(db *store.Store, feed uuid.UUID, now time.Time) (*pb.FeedApiKeyRecord, string, error) {
	if feed == uuid.Nil {
		return nil, "", ErrInvalidFeedApiKey
	}
	record, token, err := newFeedApiCredential(feed)
	if err != nil {
		return nil, "", err
	}
	record.CreatedAtMs = now.UTC().UnixMilli()
	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		existing, err := readFeedApiKey(db, feed)
		if err == nil && existing.RevokedAtMs == 0 && len(existing.SecretSha256) != 0 {
			return ErrFeedApiKeyExists
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		return stageFeedApiKey(batch, feed, record)
	})
	if err != nil {
		return nil, "", err
	}
	return proto.Clone(record).(*pb.FeedApiKeyRecord), token, nil
}

func RotateFeedApiKey(db *store.Store, feed uuid.UUID, now time.Time) (*pb.FeedApiKeyRecord, string, error) {
	if feed == uuid.Nil {
		return nil, "", ErrInvalidFeedApiKey
	}
	replacement, token, err := newFeedApiCredential(feed)
	if err != nil {
		return nil, "", err
	}
	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		existing, err := readFeedApiKey(db, feed)
		if err != nil {
			return err
		}
		if existing.RevokedAtMs != 0 || len(existing.SecretSha256) == 0 {
			return ErrFeedApiKeyInactive
		}
		replacement.CreatedAtMs = existing.CreatedAtMs
		replacement.RotatedAtMs = now.UTC().UnixMilli()
		return stageFeedApiKey(batch, feed, replacement)
	})
	if err != nil {
		return nil, "", err
	}
	return proto.Clone(replacement).(*pb.FeedApiKeyRecord), token, nil
}

func RevokeFeedApiKey(db *store.Store, feed uuid.UUID, now time.Time) (*pb.FeedApiKeyRecord, error) {
	if feed == uuid.Nil {
		return nil, ErrInvalidFeedApiKey
	}
	var result *pb.FeedApiKeyRecord
	err := db.ApplyBatch(func(batch *pebble.Batch) error {
		existing, err := readFeedApiKey(db, feed)
		if err != nil {
			return err
		}
		result = proto.Clone(existing).(*pb.FeedApiKeyRecord)
		if result.RevokedAtMs != 0 {
			return nil
		}
		result.RevokedAtMs = now.UTC().UnixMilli()
		result.SecretSha256 = nil
		return stageFeedApiKey(batch, feed, result)
	})
	if err != nil {
		return nil, err
	}
	return proto.Clone(result).(*pb.FeedApiKeyRecord), nil
}

// AuthenticateFeedApiKey returns only the token's non-secret identity. All
// malformed, unknown, rotated and revoked tokens use the same public error.
func AuthenticateFeedApiKey(db *store.Store, token string) (uuid.UUID, []byte, error) {
	parsed, err := parseFeedApiToken(token)
	if err != nil {
		return uuid.Nil, nil, ErrInvalidFeedApiKey
	}
	record, err := readFeedApiKey(db, parsed.feed)
	if err != nil || record.RevokedAtMs != 0 || len(record.KeyId) != feedApiKeyIDSize || len(record.SecretSha256) != sha256.Size {
		return uuid.Nil, nil, ErrInvalidFeedApiKey
	}
	digest := sha256.Sum256(parsed.secret)
	if subtle.ConstantTimeCompare(record.KeyId, parsed.keyID) != 1 || subtle.ConstantTimeCompare(record.SecretSha256, digest[:]) != 1 {
		return uuid.Nil, nil, ErrInvalidFeedApiKey
	}
	return parsed.feed, append([]byte(nil), record.KeyId...), nil
}
