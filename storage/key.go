package store

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"unsafe"

	uuid "github.com/satori/go.uuid"
	"github.com/yinhm/friendfeed/storage/flake"
)

type KeyPrefix uint32

var (
	// KeyMin is a minimum key value which sorts before all other keys.
	KeyMin = Key("")
	// KeyMax is a maximum key value which sorts after all other keys.
	KeyMax = Key{0xff, 0xff}
)

type Key []byte

// Copy makes a copy of the key.
func (k Key) Copy() Key {
	buf := make([]byte, len(k))
	copy(buf, k)
	return buf
}

func (k Key) Len() int {
	return len(k)
}

func (k Key) String() string {
	return hex.EncodeToString(k)
}

func KeyFromString(k string) Key {
	b, err := hex.DecodeString(k)
	// FIXME: not all string are hex encoded
	// this leads to inconsistency
	if err != nil {
		return []byte(k)
	}
	return b
}

// BytesNext returns the next possible byte slice, using the extra capacity
// of the provided slice if possible, and if not, appending an \x00.
func BytesNext(b []byte) []byte {
	if cap(b) > len(b) {
		bNext := b[:len(b)+1]
		if bNext[len(bNext)-1] == 0 {
			return bNext
		}
	}
	// TODO(spencer): Do we need to enforce KeyMaxLength here?
	// Switched to "make and copy" pattern in #4963 for performance.
	bn := make([]byte, len(b)+1)
	copy(bn, b)
	bn[len(bn)-1] = 0
	return bn
}

func bytesPrefixEnd(b []byte) []byte {
	// Switched to "make and copy" pattern in #4963 for performance.
	end := make([]byte, len(b))
	copy(end, b)
	for i := len(end) - 1; i >= 0; i-- {
		end[i] = end[i] + 1
		if end[i] != 0 {
			return end[:i+1]
		}
	}
	// This statement will only be reached if the key is already a
	// maximal byte string (i.e. already \xff...).
	return b
}

// Next returns the next key in   sort order. The method may only
// take a shallow copy of the Key, so both the receiver and the return
// value should be treated as immutable after.
func (k Key) Next() Key {
	return Key(BytesNext(k))
}

// IsPrev is a more efficient version of k.Next().Equal(m).
func (k Key) IsPrev(m Key) bool {
	l := len(m) - 1
	return l == len(k) && m[l] == 0 && k.Equal(m[:l])
}

// PrefixEnd determines the end key given key as a prefix, that is the
// key that sorts precisely behind all keys starting with prefix: "1"
// is added to the final byte and the carry propagated. The special
// cases of nil and KeyMin always returns KeyMax.
func (k Key) PrefixEnd() Key {
	if len(k) == 0 {
		return Key(KeyMax)
	}
	return Key(bytesPrefixEnd(k))
}

// Equal returns whether two keys are identical.
func (k Key) Equal(l Key) bool {
	return bytes.Equal(k, l)
}

// Compare compares the two Keys.
func (k Key) Compare(b Key) int {
	return bytes.Compare(k, b)
}

// Format implements the fmt.Formatter interface.
func (k Key) Format(f fmt.State, verb rune) {
	// Note: this implementation doesn't handle the width and precision
	// specifiers such as "%20.10s".
	if verb == 'x' {
		fmt.Fprintf(f, "%x", []byte(k))
	} else {
		fmt.Fprint(f, strconv.Quote(string(k)))
	}
}

// All keys should partitioned by 4bytes table prefix
//
// Key interface

type IKey interface {
	Prefix() IKey
	Bytes() []byte
	String() string
	Len() int
}

// KeyPrefix
func (p KeyPrefix) Bytes() []byte {
	buf := make([]byte, p.Len())
	binary.BigEndian.PutUint32(buf, uint32(p))
	return buf
}

func (p KeyPrefix) Len() int {
	return int(unsafe.Sizeof(p))
}

// Exists for satisfying Key interface
func (p KeyPrefix) Prefix() IKey {
	return p
}

func (p KeyPrefix) String() string {
	return hex.EncodeToString(p.Bytes())
}

// --------------------------------------------------
//
// Meta key, used to store meta info.
//
// Defined as following:
// +----------+----------+
// |  4bytes  |   ?bytes |
// +----------+----------+
// |  table   |  string  |
// +----------+----------+
type MetaKey struct {
	KeyPrefix
	Meta string
}

func NewMetaKey(prefix KeyPrefix, meta string) *MetaKey {
	return &MetaKey{prefix, meta}
}

func (k *MetaKey) Bytes() []byte {
	var preBytes [4]byte
	binary.BigEndian.PutUint32(preBytes[:], uint32(k.KeyPrefix))

	var buf bytes.Buffer
	buf.Write(preBytes[:])
	buf.Write([]byte(k.Meta))
	return buf.Bytes()
}

func (k *MetaKey) Len() int {
	return k.KeyPrefix.Len() + len(k.Meta)
}

func (k *MetaKey) Prefix() IKey {
	return k.KeyPrefix
}

func (k *MetaKey) String() string {
	return hex.EncodeToString(k.Prefix().Bytes()) + k.Meta
}

// Defined as following:
// +----------+----------+
// |  4bytes  |  16bytes |
// +----------+----------+
// |  table   | flake id |
// +----------+----------+
type FlakeKey struct {
	KeyPrefix
	Id flake.Id
}

func NewFlakeKey(prefix KeyPrefix, id flake.Id) *FlakeKey {
	return &FlakeKey{prefix, id}
}

func (k *FlakeKey) Bytes() []byte {
	buf := new(bytes.Buffer)
	// nothing we can do if cannot allocate memory
	if err := binary.Write(buf, binary.BigEndian, k.KeyPrefix); err != nil {
		panic(err)
	}
	if err := binary.Write(buf, binary.BigEndian, k.Id); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func (k *FlakeKey) Len() int {
	return k.KeyPrefix.Len() + len(k.Id)
}

func (k *FlakeKey) Prefix() IKey {
	return k.KeyPrefix
}

func (k *FlakeKey) String() string {
	return hex.EncodeToString(k.Bytes())
}

// UUID Key.
//
// +----------+----------+
// |  4bytes  |  16bytes |
// +----------+----------+
// |  table   |   uuid   |
// +----------+----------+
type UUIDKey struct {
	KeyPrefix
	uuid uuid.UUID //[16]byte
}

func NewUUIDKey(prefix KeyPrefix, id uuid.UUID) *UUIDKey {
	return &UUIDKey{prefix, id}
}

func (k *UUIDKey) Bytes() []byte {
	var buf bytes.Buffer
	var tb [4]byte
	binary.BigEndian.PutUint32(tb[:], uint32(k.KeyPrefix))
	buf.Write(tb[:])
	buf.Write(k.uuid[:])
	return buf.Bytes()
}

func (k *UUIDKey) Len() int {
	return int(unsafe.Sizeof(k.uuid)) + k.Prefix().Len()
}

func (k *UUIDKey) Prefix() IKey {
	return k.KeyPrefix
}

func (k *UUIDKey) String() string {
	return hex.EncodeToString(k.Bytes())
}

// UUID Flake Key.
//
// +----------+----------+----------+
// |  4bytes  |  16bytes |  16bytes |
// +----------+----------+----------+
// |  table   |   uuid   | flake id |
// +----------+----------+----------+
type UUIDFlakeKey struct {
	UUIDKey
	Id flake.Id
}

func NewUUIDFlakeKey(prefix KeyPrefix, uuid uuid.UUID, id flake.Id) *UUIDFlakeKey {
	uk := UUIDKey{prefix, uuid}
	return &UUIDFlakeKey{uk, id}
}

func (k *UUIDFlakeKey) Bytes() []byte {
	var preBytes [4]byte
	binary.BigEndian.PutUint32(preBytes[:], uint32(k.KeyPrefix))

	var buf bytes.Buffer
	buf.Write(preBytes[:])
	buf.Write(k.uuid[:])
	buf.Write(k.Id[:])
	return buf.Bytes()
}

func (k *UUIDFlakeKey) Len() int {
	return k.UUIDKey.Len() + int(unsafe.Sizeof(k.Id))
}

func (k *UUIDFlakeKey) Prefix() IKey {
	return &k.UUIDKey
}

func (k *UUIDFlakeKey) String() string {
	return hex.EncodeToString(k.Bytes())
}
