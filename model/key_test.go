package model

import (
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/gofrs/uuid"
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
	symbolUUID := UniqueKeyFrom(market, symbol)
	key1 := store.NewUUIDFlakeKey(TableKLine, symbolUUID, flakeid)

	uuid2 := UniqueKeyFrom(market, symbol)
	key2 := NewPrefixKeyFrom(TableKLine, uuid2.Bytes())

	assert.Equal(t, "093cc15911635c5c822f0b31db5089f8", fmt.Sprintf("%x", symbolUUID))
	assert.Equal(t, symbolUUID, uuid2)
	assert.Contains(t, key1.String(), key2.String())
	assert.Equal(t, 20, len(key2))
	for i := range 20 {
		assert.Equal(t, key1.Bytes()[i], key2[i])
	}
}

func TestTimelineUUIDPreservesExistingKey(t *testing.T) {
	userUUID := uuid.Must(uuid.FromString("c6f8dca854f011ddb489003048343a40"))
	want := UniqueKeyFrom(fmt.Sprintf("%x", userUUID), "user", "timeline")
	assert.Equal(t, want, TimelineUUID(userUUID))
}
