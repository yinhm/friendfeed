package store

import (
	"encoding/hex"
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/storage/flake"
)

//-------------------------
// testing keys
//-------------------------
func TestKeyPrefix(t *testing.T) {
	// Giving prefix table, convert to bytes
	var p1 KeyPrefix
	assert.Equal(t, 4, p1.Len())

	p := TableFeed
	assert.Equal(t, 4, p.Len())
	assert.Equal(t, "00000001", p.String())
	assert.Equal(t, "00000001", hex.EncodeToString(p.Bytes()))
}

func TestMetaKey(t *testing.T) {
	// Giving meta key, When convert to bytes
	key := &MetaKey{TableOAuthTwitter, "foobar"}
	assert.Equal(t, 10, key.Len())
	assert.Equal(t, 4, key.Prefix().Len())
	assert.Equal(t, key.Prefix().String()+"foobar", key.String())
}

func TestFlakeKey(t *testing.T) {
	// Giving falke key, When convert to bytes
	fid := flake.Id{}
	suffix := hex.EncodeToString(fid[:])
	key := &FlakeKey{TableFeed, fid}
	assert.Equal(t, 20, key.Len())
	assert.Equal(t, 4, key.Prefix().Len())
	assert.Equal(t, "00000001"+suffix, key.String())
	assert.Equal(t, "00000001", hex.EncodeToString(key.Prefix().Bytes()))

	key.KeyPrefix = TableFeedinfo
	assert.Equal(t, "00000002"+suffix, key.String())
	assert.Equal(t, "00000002", hex.EncodeToString(key.Prefix().Bytes()))

	key.Id[15] = 1
	suffix = hex.EncodeToString(key.Id[:])
	assert.Equal(t, key.String(), "00000002"+suffix)
	assert.Equal(t, hex.EncodeToString(key.Prefix().Bytes()), "00000002")
}

func TestUUIDKey(t *testing.T) {
	// Giving prefix, convert to bytes
	uuid1 := new(uuid.UUID)
	assert.Equal(t, uuid1.String(), "00000000-0000-0000-0000-000000000000")

	id, err := uuid.FromString("c6f8dca8-54f0-11dd-b489-003048343a40")
	assert.Nil(t, err)
	assert.Equal(t, hex.EncodeToString(id.Bytes()), hex.EncodeToString(id[:16][:]))

	prefix := NewUUIDKey(TableFeed, id)
	assert.Equal(t, prefix.Len(), 20)

	uid := "c6f8dca854f011ddb489003048343a40"
	assert.Equal(t, prefix.String(), "00000001"+uid)
	assert.Equal(t, hex.EncodeToString(prefix.Bytes()), "00000001"+uid)

	u1 := uuid.NewV5(uuid.NamespaceURL, "tarrier")
	u2 := uuid.NewV5(uuid.NamespaceURL, "tarrier")
	assert.Equal(t, u1, u2)
}

func TestUUIDFlakeKey(t *testing.T) {
	// Giving key, convert to bytes
	id, err := uuid.FromString("c6f8dca8-54f0-11dd-b489-003048343a40")
	assert.Nil(t, err)
	assert.Equal(t, hex.EncodeToString(id.Bytes()), hex.EncodeToString(id[:16][:]))

	fid := flake.Id{}
	suffix := hex.EncodeToString(fid[:])
	key := NewUUIDFlakeKey(TableFeed, id, fid)
	assert.Equal(t, key.Len(), 36)

	uid := "c6f8dca854f011ddb489003048343a40"
	assert.Equal(t, key.String(), "00000001"+uid+suffix)
	assert.Equal(t, hex.EncodeToString(key.Prefix().Bytes()), "00000001"+uid)
	assert.Equal(t, string(key.Prefix().Bytes()), string(key.UUIDKey.Bytes()))

	key.UUIDKey.KeyPrefix = TableFeedinfo
	assert.Equal(t, key.String(), "00000002"+uid+suffix)
	assert.Equal(t, hex.EncodeToString(key.Prefix().Bytes()), "00000002"+uid)

	key.Id[15] = 1
	suffix = hex.EncodeToString(key.Id[:])
	assert.Equal(t, key.String(), "00000002"+uid+suffix)
	assert.Equal(t, hex.EncodeToString(key.Prefix().Bytes()), "00000002"+uid)
}
