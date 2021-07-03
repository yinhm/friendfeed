package model

import (
	"bytes"
	"encoding/binary"

	uuid "github.com/satori/go.uuid"
	store "github.com/yinhm/friendfeed/storage"
)

// UUID Key.
//
// +----------+----------+
// |  4bytes  |  16bytes |
// +----------+----------+
// |  table   |   uuid   |
// +----------+----------+
func NewUUIDKey(t store.KeyPrefix) store.Key {
	// blank key
	u := uuid.NewV4()

	b := KeyPrefixToBytes(t)
	return NewKeyFrom(b, u[:])
}

func NewPrefixKeyFrom(t store.KeyPrefix, u []byte) store.Key {
	b := KeyPrefixToBytes(t)
	return NewKeyFrom(b, u)
}

func NewKeyFrom(bs ...[]byte) store.Key {
	var buf bytes.Buffer
	for _, b := range bs {
		buf.Write(b[:])
	}
	return buf.Bytes()
}

func KeyPrefixToBytes(t store.KeyPrefix) []byte {
	var bytes [4]byte
	binary.BigEndian.PutUint32(bytes[:], uint32(t))
	return bytes[:]
}

// func NewStockKey() store.Key {
// 	return NewUUIDKey(TableStock)
// }

// Agent Key.
//
// +----------+----------+----------+
// |  4bytes  |  16bytes |  20bytes |
// +----------+----------+----------+
// |  table   | farmuuid |   uid    |
// +----------+----------+----------+
// func NewAgentKey(farmKey, uid []byte) Key {
// 	pre := KeyPrefixToBytes(TableAgent)
// 	return NewKeyFrom(pre, farmKey[4:], uid)
// }

func SeekZero() []byte {
	u := new(uuid.UUID)
	return u[:]
}
