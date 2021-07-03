package model

import (
	"bytes"
	"encoding/binary"
	"log"

	"github.com/gofrs/uuid"
	store "github.com/yinhm/friendfeed/storage"
)

// UUID Key.
//
// +----------+----------+
// |  4bytes  |  16bytes |
// +----------+----------+
// |  table   |   uuid   |
// +----------+----------+
func NewUUIDKey(t KeyPrefix) store.Key {
	u, err := uuid.NewV4()
	if err != nil {
		log.Fatalf("failed to generate UUID: %v", err)
	}

	b := KeyPrefixToBytes(t)
	return NewKeyFrom(b, u[:])
}

func NewPrefixKeyFrom(t KeyPrefix, u []byte) store.Key {
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

func KeyPrefixToBytes(t KeyPrefix) []byte {
	var bytes [4]byte
	binary.BigEndian.PutUint32(bytes[:], uint32(t))
	return bytes[:]
}

func NewStockKey() store.Key {
	return NewUUIDKey(TableStock)
}

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
