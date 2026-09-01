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
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

const (
	importOperatorTokenVersion = "ffo1"
	importOperatorKeyIDSize    = 8
	importOperatorSecretSize   = 32
	MaxImportOperatorTokenTTL  = time.Hour
)

var (
	importOperatorTokenKey  = NewKeyFrom(TableMeta.Bytes(), []byte("import-operator-token/v1"))
	ErrInvalidOperatorToken = errors.New("invalid import operator token")
	ErrInvalidOperatorTTL   = errors.New("import operator token TTL must be between 1 second and 1 hour")
)

func ImportOperatorTokenMetaKey() store.Key {
	return append(store.Key(nil), importOperatorTokenKey...)
}

func encodeImportOperatorToken(keyID, secret []byte) string {
	enc := base64.RawURLEncoding
	return importOperatorTokenVersion + "_" + enc.EncodeToString(keyID) + "_" + enc.EncodeToString(secret)
}

func parseImportOperatorToken(token string) ([]byte, []byte, error) {
	const totalLen = len(importOperatorTokenVersion) + 1 + 11 + 1 + 43
	if len(token) != totalLen || !strings.HasPrefix(token, importOperatorTokenVersion+"_") || token[16] != '_' {
		return nil, nil, ErrInvalidOperatorToken
	}
	decode := func(raw string, size int) ([]byte, error) {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(decoded) != size {
			return nil, ErrInvalidOperatorToken
		}
		return decoded, nil
	}
	keyID, err := decode(token[5:16], importOperatorKeyIDSize)
	if err != nil {
		return nil, nil, err
	}
	secret, err := decode(token[17:], importOperatorSecretSize)
	return keyID, secret, err
}

func readImportOperatorToken(db *store.Store) (*pb.ImportOperatorTokenRecord, error) {
	raw, err := db.Get(importOperatorTokenKey)
	if err != nil {
		return nil, err
	}
	record := new(pb.ImportOperatorTokenRecord)
	if err := proto.Unmarshal(raw, record); err != nil {
		return nil, err
	}
	return record, nil
}

func stageImportOperatorToken(batch *pebble.Batch, record *pb.ImportOperatorTokenRecord) error {
	raw, err := proto.Marshal(record)
	if err != nil {
		return err
	}
	return batch.Set(importOperatorTokenKey, raw, nil)
}

func GetImportOperatorToken(db *store.Store) (*pb.ImportOperatorTokenRecord, error) {
	record, err := readImportOperatorToken(db)
	if err != nil {
		return nil, err
	}
	return proto.Clone(record).(*pb.ImportOperatorTokenRecord), nil
}

// IssueImportOperatorToken replaces the sole active operator credential.
func IssueImportOperatorToken(db *store.Store, now time.Time, ttl time.Duration, issuedBy string) (*pb.ImportOperatorTokenRecord, string, error) {
	if ttl < time.Second || ttl > MaxImportOperatorTokenTTL {
		return nil, "", ErrInvalidOperatorTTL
	}
	keyID := make([]byte, importOperatorKeyIDSize)
	secret := make([]byte, importOperatorSecretSize)
	if _, err := rand.Read(keyID); err != nil {
		return nil, "", fmt.Errorf("generate import operator key ID: %w", err)
	}
	if _, err := rand.Read(secret); err != nil {
		return nil, "", fmt.Errorf("generate import operator secret: %w", err)
	}
	digest := sha256.Sum256(secret)
	record := &pb.ImportOperatorTokenRecord{
		KeyId: keyID, SecretSha256: digest[:], CreatedAtMs: now.UTC().UnixMilli(),
		ExpiresAtMs: now.UTC().Add(ttl).UnixMilli(), IssuedBy: issuedBy,
	}
	if err := db.ApplyBatch(func(batch *pebble.Batch) error { return stageImportOperatorToken(batch, record) }); err != nil {
		return nil, "", err
	}
	return proto.Clone(record).(*pb.ImportOperatorTokenRecord), encodeImportOperatorToken(keyID, secret), nil
}

func RevokeImportOperatorToken(db *store.Store, now time.Time) (*pb.ImportOperatorTokenRecord, error) {
	var result *pb.ImportOperatorTokenRecord
	err := db.ApplyBatch(func(batch *pebble.Batch) error {
		record, err := readImportOperatorToken(db)
		if err != nil {
			return err
		}
		result = proto.Clone(record).(*pb.ImportOperatorTokenRecord)
		if result.RevokedAtMs != 0 {
			return nil
		}
		result.RevokedAtMs = now.UTC().UnixMilli()
		result.SecretSha256 = nil
		return stageImportOperatorToken(batch, result)
	})
	if err != nil {
		return nil, err
	}
	return proto.Clone(result).(*pb.ImportOperatorTokenRecord), nil
}

func AuthenticateImportOperatorToken(db *store.Store, token string, now time.Time) ([]byte, error) {
	keyID, secret, err := parseImportOperatorToken(token)
	if err != nil {
		return nil, ErrInvalidOperatorToken
	}
	record, err := readImportOperatorToken(db)
	if err != nil || record.RevokedAtMs != 0 || now.UTC().UnixMilli() >= record.ExpiresAtMs ||
		len(record.KeyId) != importOperatorKeyIDSize || len(record.SecretSha256) != sha256.Size {
		return nil, ErrInvalidOperatorToken
	}
	digest := sha256.Sum256(secret)
	if subtle.ConstantTimeCompare(record.KeyId, keyID) != 1 || subtle.ConstantTimeCompare(record.SecretSha256, digest[:]) != 1 {
		return nil, ErrInvalidOperatorToken
	}
	return append([]byte(nil), record.KeyId...), nil
}
