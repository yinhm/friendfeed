package store

import (
	"encoding/hex"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
)

func TestUUIDKey(t *testing.T) {
	uuid1 := new(uuid.UUID)
	assert.Equal(t, uuid1.String(), "00000000-0000-0000-0000-000000000000")

	id, err := uuid.FromString("c6f8dca8-54f0-11dd-b489-003048343a40")
	assert.Nil(t, err)
	assert.Equal(t, hex.EncodeToString(id.Bytes()), hex.EncodeToString(id[:16][:]))

	u1 := uuid.NewV5(uuid.NamespaceURL, "tarrier")
	u2 := uuid.NewV5(uuid.NamespaceURL, "tarrier")
	assert.Equal(t, u1, u2)
}

func TestFarmKey(t *testing.T) {
	farmHash := "00000065eab3360472a0425ab5f214afc8ed5d7a"
	key := KeyFromString(farmHash)

	ks := key.String()
	assert.Equal(t, ks, farmHash)
	assert.Equal(t, 20, key.Len())
	assert.Equal(t, 20, len(key))

	// ks = "config1"
	// key = KeyFromString(ks)
	// assert.Equal(t, ks, key.String())
}
