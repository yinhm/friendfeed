package model

import (
	"bytes"
	"encoding/hex"

	"github.com/gofrs/uuid"
	"github.com/golang/protobuf/proto"
	store "github.com/yinhm/friendfeed/storage"
)

type Table struct {
	db      *store.Store
	prefix  store.Key
	preSize int

	NewMessage ProtoMessageFunc
}

func NewTable(prefix store.Key) *Table {
	return &Table{
		prefix:  prefix,
		preSize: prefix.Len(),
	}
}

func (t *Table) InitStore(store *store.Store) {
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
	k := t.prefixKey(store.KeyFromString(key))
	bytes, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return t.db.Set(k, bytes)
}

func (t *Table) Delete(key string) error {
	k := t.prefixKey(store.KeyFromString(key))
	return t.db.Delete(k)
}

func (t *Table) Keys(ks ...string) (keys []string, err error) {
	var buf bytes.Buffer
	buf.Write(t.prefix)
	if len(ks) > 0 {
		for _, k := range ks {
			buf.Write(store.KeyFromString(k)[:])
		}
	}
	buf.Write(SeekZero())
	start := buf.Bytes()

	iter := t.db.NewIterator(start)
	for iter.First(); iter.Valid(); iter.Next() {
		keys = append(keys, t.toStringKey(iter.Key()))
	}

	return keys, err
}

func (t *Table) Iter(fn func(raw []byte) error) error {
	iter := t.db.NewIterator(t.prefix)
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
	buf.Write(store.KeyFromString(fHash)[:])
	kp := buf.Bytes()

	iter := t.db.NewIterator(kp)
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		value := iter.Value()
		if err := fn(value); err != nil {
			return err
		}
	}
	return nil
}
