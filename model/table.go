package model

import (
	"bytes"
	"encoding/hex"

	"github.com/gofrs/uuid"
	"github.com/golang/protobuf/proto"
	store "github.com/yinhm/friendfeed/storage"
)

var (
	TableInited bool

	TConfig *Table
	TStock  *Table
)

func init() {
	TableInited = false

	TConfig = NewTable(KeyPrefixToBytes(TableConfig))
	TStock = NewTable(KeyPrefixToBytes(TableStock))
}

// func() proto.Message { return new(pb.LoginReq)
type ProtoMessageFunc func() proto.Message

type Table struct {
	db      *Store
	prefix  store.Key
	preSize int

	// Deprecated: need more clean api.
	// fn = func() proto.Message {
	// 	return new(pb.Farm)
	// }
	NewMessage ProtoMessageFunc
}

func NewTable(prefix store.Key) *Table {
	return &Table{
		prefix:  prefix,
		preSize: prefix.Len(),
	}
}

func (t *Table) InitStore(store *Store) {
	t.db = store
}

// UUID Key.
//
// +----------+----------+
// |  4bytes  |  16bytes |
// +----------+----------+
// |  table   |   uuid   |
// +----------+----------+
//
// -----------------------
//
// Prefix are internal
// return only uuid part
//   of the key.
//
// +----------+
// |  16bytes |
// +----------+
// |   uuid   |
// +----------+
func (t *Table) NewKey(name string) string {
	u := uuid.NewV5(uuid.NamespaceURL, name)
	return hex.EncodeToString(u[:])
}

func (t *Table) prefixKey(key store.Key) store.Key {
	return NewKeyFrom(t.prefix, key)
}

func (t *Table) removePrefixKey(key store.Key) store.Key {
	return key[t.preSize:]
}

func (t *Table) toStringKey(key store.Key) string {
	return t.removePrefixKey(key).String()
}

func (t *Table) Get(key string, msg proto.Message) error {
	k := t.prefixKey(store.KeyFromString(key))
	raw, err := t.db.Get(k)
	if err != nil {
		return err
	}
	return proto.Unmarshal(raw, msg)
}

func (t *Table) Put(key string, msg proto.Message) error {
	k := t.prefixKey(KeyFromString(key))
	bytes, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return t.db.Set(k, bytes)
}

func (t *Table) Delete(key string) error {
	k := t.prefixKey(KeyFromString(key))
	return t.db.Delete(k)
}

func (t *Table) Keys(ks ...string) (keys []string, err error) {
	var buf bytes.Buffer
	buf.Write(t.prefix)
	if len(ks) > 0 {
		for _, k := range ks {
			buf.Write(KeyFromString(k)[:])
		}
	}
	buf.Write(SeekZero())
	start := buf.Bytes()

	opts := prefixIteratorOptions(start)
	iter := t.db.bdb.NewIter(opts)
	for iter.First(); iter.Valid(); iter.Next() {
		keys = append(keys, t.toStringKey(iter.Key()))
	}

	return keys, err
}

func (t *Table) Iter(fn func(raw []byte) error) error {
	opts := prefixIteratorOptions(t.prefix)
	iter := newIterator(t.db.bdb, opts)
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		value := iter.Value()
		if err := fn(value); err != nil {
			return err
		}
	}
	return nil
}

// find agents etc in farm
func (t *Table) Find(fHash string, fn func(raw []byte) error) error {
	var buf bytes.Buffer
	buf.Write(t.prefix)
	buf.Write(KeyFromString(fHash)[:])
	kp := buf.Bytes()

	opts := prefixIteratorOptions(kp)
	iter := newIterator(t.db.bdb, opts)
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		value := iter.Value()
		if err := fn(value); err != nil {
			return err
		}
	}
	return nil
}
