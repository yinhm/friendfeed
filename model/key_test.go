package model

import (
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/store"
)

func TestTimeParse(t *testing.T) {
	// "Given RFC3339, parse time string"
	// Z           A suffix which, when applied to a time, denotes a UTC
	//             offset of 00:00; often spoken "Zulu" from the ICAO
	//             phonetic alphabet representation of the letter "Z".
	dt := "2009-06-25T18:23:38Z"
	got, _ := time.Parse(time.RFC3339, dt)

	assert.Equal(t, 2009, got.Year())
	assert.Equal(t, 18, got.Hour())
	assert.Equal(t, 18, got.UTC().Hour())
}

func TestTimeFormat(t *testing.T) {
	// Given time, format RFC3339 string
	dt := "2009-06-25T18:23:38Z"
	rfcTime, _ := time.Parse(time.RFC3339, dt)
	got := rfcTime.Format(time.RFC3339)
	assert.Equal(t, dt, got)
}

//-------------------------
// testing keys
//-------------------------

func TestKeyPrefix(t *testing.T) {
	p := TableFeed
	assert.Equal(t, 4, p.Len())
	assert.Equal(t, "00000001", p.String())
	assert.Equal(t, "00000001", hex.EncodeToString(p.Bytes()))
}

func TestMetaKey(t *testing.T) {
	// Giving meta key, When convert to bytes
	key := NewPrefixKeyFrom(TableOAuth, []byte("foobar"))

	// key := &MetaKey{TableOAuthTwitter, "foobar"}
	assert.Equal(t, 10, key.Len())
	// hex decoded, this is diff from MetaKey...
	assert.Equal(t, "00000068666f6f626172", key.String())
}

func TestUniqueKey(t *testing.T) {
	db := &store.Store{}
	dt := time.Date(2021, 7, 25, 15, 0, 0, 0, time.UTC)
	flakeid := db.TimeTravelReverseId(dt)
	// assert.Equal(t, "000006aee6c8de801c697aa5a6ca0000", fmt.Sprintf("%x", flakeid))
	assert.Equal(t, "000006aee6c8de8042010af000030000", fmt.Sprintf("%x", flakeid))

	market, symbol := "SH", "600519"
	uuid1 := UniqueKeyFrom(market, symbol)
	key1 := store.NewUUIDFlakeKey(TableKLine, uuid1, flakeid)

	uuid2 := UniqueKeyFrom(market, symbol)
	key2 := NewPrefixKeyFrom(TableKLine, uuid2.Bytes())

	assert.Equal(t, "093cc15911635c5c822f0b31db5089f8", fmt.Sprintf("%x", uuid1))
	assert.Equal(t, uuid1, uuid2)
	assert.Contains(t, key1.String(), key2.String())
	assert.Equal(t, 20, len(key2))
	for i := 0; i < 20; i++ {
		assert.Equal(t, key1.Bytes()[i], key2[i])
	}
}

//-------------------------
// testing keys
//-------------------------
// func TestKeyPrefix(t *testing.T) {
// 	// Giving prefix table, convert to bytes
// 	var p1 KeyPrefix
// 	assert.Equal(t, 4, p1.Len())
// }

// func TestMetaKey(t *testing.T) {
// 	// Giving meta key, When convert to bytes
// 	key := &MetaKey{TableOAuthTwitter, "foobar"}
// 	assert.Equal(t, 10, key.Len())
// 	assert.Equal(t, 4, key.Prefix().Len())
// 	assert.Equal(t, key.Prefix().String()+"foobar", key.String())
// }

// func TestFlakeKey(t *testing.T) {
// 	// Giving falke key, When convert to bytes
// 	fid := flake.Id{}
// 	suffix := hex.EncodeToString(fid[:])
// 	key := &FlakeKey{TableFeed, fid}
// 	assert.Equal(t, 20, key.Len())
// 	assert.Equal(t, 4, key.Prefix().Len())
// 	assert.Equal(t, "00000001"+suffix, key.String())
// 	assert.Equal(t, "00000001", hex.EncodeToString(key.Prefix().Bytes()))

// 	key.KeyPrefix = TableFeedinfo
// 	assert.Equal(t, "00000002"+suffix, key.String())
// 	assert.Equal(t, "00000002", hex.EncodeToString(key.Prefix().Bytes()))

// 	key.Id[15] = 1
// 	suffix = hex.EncodeToString(key.Id[:])
// 	assert.Equal(t, key.String(), "00000002"+suffix)
// 	assert.Equal(t, hex.EncodeToString(key.Prefix().Bytes()), "00000002")
// }

// func TestUUIDKey(t *testing.T) {
// 	// Giving prefix, convert to bytes
// 	uuid1 := new(uuid.UUID)
// 	assert.Equal(t, uuid1.String(), "00000000-0000-0000-0000-000000000000")

// 	id, err := uuid.FromString("c6f8dca8-54f0-11dd-b489-003048343a40")
// 	assert.Nil(t, err)
// 	assert.Equal(t, hex.EncodeToString(id.Bytes()), hex.EncodeToString(id[:16][:]))

// 	prefix := NewUUIDKey(TableFeed, id)
// 	assert.Equal(t, prefix.Len(), 20)

// 	uid := "c6f8dca854f011ddb489003048343a40"
// 	assert.Equal(t, prefix.String(), "00000001"+uid)
// 	assert.Equal(t, hex.EncodeToString(prefix.Bytes()), "00000001"+uid)

// 	u1 := uuid.NewV5(uuid.NamespaceURL, "tarrier")
// 	u2 := uuid.NewV5(uuid.NamespaceURL, "tarrier")
// 	assert.Equal(t, u1, u2)
// }

// func TestUUIDFlakeKey(t *testing.T) {
// 	// Giving key, convert to bytes
// 	id, err := uuid.FromString("c6f8dca8-54f0-11dd-b489-003048343a40")
// 	assert.Nil(t, err)
// 	assert.Equal(t, hex.EncodeToString(id.Bytes()), hex.EncodeToString(id[:16][:]))

// 	fid := flake.Id{}
// 	suffix := hex.EncodeToString(fid[:])
// 	key := NewUUIDFlakeKey(TableFeed, id, fid)
// 	assert.Equal(t, key.Len(), 36)

// 	uid := "c6f8dca854f011ddb489003048343a40"
// 	assert.Equal(t, key.String(), "00000001"+uid+suffix)
// 	assert.Equal(t, hex.EncodeToString(key.Prefix().Bytes()), "00000001"+uid)
// 	assert.Equal(t, string(key.Prefix().Bytes()), string(key.UUIDKey.Bytes()))

// 	key.UUIDKey.KeyPrefix = TableFeedinfo
// 	assert.Equal(t, key.String(), "00000002"+uid+suffix)
// 	assert.Equal(t, hex.EncodeToString(key.Prefix().Bytes()), "00000002"+uid)

// 	key.Id[15] = 1
// 	suffix = hex.EncodeToString(key.Id[:])
// 	assert.Equal(t, key.String(), "00000002"+uid+suffix)
// 	assert.Equal(t, hex.EncodeToString(key.Prefix().Bytes()), "00000002"+uid)
// }
