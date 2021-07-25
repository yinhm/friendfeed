package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	store "github.com/yinhm/friendfeed/storage"
)

//-------------------------
// testing keys
//-------------------------
func TestUniqueKey(t *testing.T) {
	db := &store.Store{}
	dt := time.Date(2021, 7, 25, 15, 0, 0, 0, time.UTC)
	flakeid := db.TimeTravelReverseId(dt)
	assert.Equal(t, "000006aee6c8de801c697aa5a6ca0000", fmt.Sprintf("%x", flakeid))

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
