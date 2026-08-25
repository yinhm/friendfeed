package model

import (
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
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

func TestUserRenameMapKeyEncoding(t *testing.T) {
	key := UserRenameMap.PrefixAppend([]byte("oldname"))

	assert.Equal(t, "000000076f6c646e616d65", key.String())
	assert.Equal(t, "oldname", string(UserRenameMap.PrefixRemove(key)))
}

func TestTimelineUUIDPreservesExistingKey(t *testing.T) {
	userUUID := uuid.Must(uuid.FromString("c6f8dca854f011ddb489003048343a40"))
	want := UniqueKeyFrom(fmt.Sprintf("%x", userUUID), "user", "timeline")
	assert.Equal(t, want, TimelineUUID(userUUID))
}
