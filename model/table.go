package model

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gofrs/uuid"
	"github.com/golang/protobuf/proto"
	store "github.com/yinhm/friendfeed/storage"
)

type Table struct {
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

func (t *Table) prefixAppend(key store.Key) store.Key {
	return NewKeyFrom(t.prefix, key)
}

func (t *Table) prefixRemove(key store.Key) store.Key {
	return key[t.preSize:]
}

func (t *Table) toStringKey(key store.Key) string {
	return t.prefixRemove(key).String()
}

func (t *Table) Get(db *store.Store, key store.Key, msg proto.Message) error {
	k := t.prefixAppend(key)
	raw, err := db.Get(k)
	// log.Printf("db.Get(%s,...), %v", k.String(), raw)
	if err != nil {
		return fmt.Errorf("Get key <%s> error: %s", key, err)
	}
	if raw == nil {
		return ErrNotFound
	}
	return proto.Unmarshal(raw, msg)
}

func (t *Table) Put(db *store.Store, key store.Key, msg proto.Message) (store.Key, error) {
	k := t.prefixAppend(key)
	bytes, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	// log.Printf("db.Set(%s,...), %v", k.String(), msg)
	return k, db.Set(k, bytes)
}

// blind delete, no error if not exists
func (t *Table) Delete(db *store.Store, key store.Key) error {
	k := t.prefixAppend(key)
	return db.Delete(k)
}

// Reversed Entry index:
// K-> | table | user uuid | maxtime - ts-flake |
// V-> |      +++++   indexed key   ++++++      |
// value are prefixed key which point to data
func (t *Table) Index(db *store.Store, uuid1 uuid.UUID, oldtime time.Time, entryKey store.Key) error {
	flakeid := db.TimeTravelReverseId(oldtime)
	k := store.NewUUIDFlakeKey(TableEntryIndex, uuid1, flakeid)
	return db.Put(k.Bytes(), entryKey)
}

func (t *Table) Keys(db *store.Store, ks ...string) (keys []string, err error) {
	var buf bytes.Buffer
	buf.Write(t.prefix)
	if len(ks) > 0 {
		for _, k := range ks {
			buf.Write(store.KeyFromString(k)[:])
		}
	}
	buf.Write(SeekZero())
	start := buf.Bytes()

	iter := db.NewIterator(start)
	for iter.First(); iter.Valid(); iter.Next() {
		keys = append(keys, t.toStringKey(iter.Key()))
	}

	return keys, err
}

func (t *Table) Iter(db *store.Store, fn func(raw []byte) error) error {
	iter := db.NewIterator(t.prefix)
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
func (t *Table) Find(db *store.Store, fHash string, fn func(raw []byte) error) error {
	var buf bytes.Buffer
	buf.Write(t.prefix)
	buf.Write(store.KeyFromString(fHash)[:])
	kp := buf.Bytes()

	iter := db.NewIterator(kp)
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		value := iter.Value()
		if err := fn(value); err != nil {
			return err
		}
	}
	return nil
}
