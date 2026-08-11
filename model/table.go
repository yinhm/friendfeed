package model

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/store/flake"
	"google.golang.org/protobuf/proto"
)

type Table struct {
	Prefix  store.Key
	preSize int

	NewMessage ProtoMessageFunc
}

func NewTable(prefix store.Key) *Table {
	return &Table{
		Prefix:  prefix,
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
//
//	of the key.
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

func (t *Table) PrefixAppend(key store.Key) store.Key {
	return NewKeyFrom(t.Prefix, key)
}

func (t *Table) PrefixRemove(key store.Key) store.Key {
	return key[t.preSize:]
}

func (t *Table) ToStringKey(key store.Key) string {
	return t.PrefixRemove(key).String()
}

func (t *Table) Get(db *store.Store, key store.Key, msg proto.Message) error {
	k := t.PrefixAppend(key)
	raw, err := db.Get(k)
	// log.Printf("db.Get(%s,...), %v", k.String(), raw)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("Get key <%s> error: %w", key, err)
	}
	return proto.Unmarshal(raw, msg)
}

func (t *Table) GetRaw(db *store.Store, key store.Key) ([]byte, error) {
	k := t.PrefixAppend(key)
	return db.Get(k)
}

func (t *Table) Put(db *store.Store, key store.Key, msg proto.Message) (store.Key, error) {
	k := t.PrefixAppend(key)
	encoded, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	// log.Printf("db.Set(%s,...), %v", k.String(), msg)
	return k, db.Set(k, encoded)
}

// blind delete, no error if not exists
func (t *Table) Delete(db *store.Store, key store.Key) error {
	k := t.PrefixAppend(key)
	return db.Delete(k)
}

// Reversed Entry index:
// K-> | table | user uuid | Maxime - ts-flake |
// V-> |      +++++   entry key   ++++++      |
func (t *Table) Index(db *store.Store, indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) error {
	flakeid := db.TimeTravelReverseId(oldtime)
	k := store.NewUUIDFlakeKey(TableEntryIndex, indexUUID, flakeid)

	if _, err := db.ForwardScan(entryIndexTimestampPrefix(k), func(i int, k, v []byte) error {
		// log.Printf("db.Delete(%x, %x)", k, v)
		return db.Delete(k)
	}); err != nil {
		return fmt.Errorf("remove existing entry index: %w", err)
	}

	return db.Put(k.Bytes(), entryKey)
}

// indexBatch preserves Index's key encoding and duplicate-removal semantics
// while adding its mutations to the caller's atomic batch.
func (t *Table) indexBatch(db *store.Store, batch *pebble.Batch, indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) error {
	flakeid := db.TimeTravelReverseId(oldtime)
	k := store.NewUUIDFlakeKey(TableEntryIndex, indexUUID, flakeid)

	if _, err := db.ForwardScan(entryIndexTimestampPrefix(k), func(i int, oldKey, v []byte) error {
		return batch.Delete(oldKey, nil)
	}); err != nil {
		return fmt.Errorf("remove existing entry index: %w", err)
	}
	return batch.Set(k.Bytes(), entryKey, nil)
}

func entryIndexTimestampPrefix(k *store.UUIDFlakeKey) []byte {
	var layout flake.Generator
	uniquenessSuffixSize := len(layout.WorkerId) + binary.Size(layout.Sequence)
	return k.Bytes()[:k.Len()-uniquenessSuffixSize]
}

func (t *Table) RemoveIndex(db *store.Store, indexUUID uuid.UUID, oldtime time.Time) error {
	flakeid := db.TimeTravelReverseId(oldtime)
	k := store.NewUUIDFlakeKey(TableEntryIndex, indexUUID, flakeid)
	return db.Delete(k.Bytes())
}

func (t *Table) Keys(db *store.Store, ks ...string) (keys []string, err error) {
	var buf bytes.Buffer
	buf.Write(t.Prefix)
	if len(ks) > 0 {
		for _, k := range ks {
			buf.Write(store.KeyFromString(k))
		}
	}
	buf.Write(SeekZero())
	start := buf.Bytes()

	iter, err := db.NewIterator(start)
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		keys = append(keys, t.ToStringKey(iter.Key()))
	}
	return keys, iter.Error()
}

func (t *Table) Iter(db *store.Store, fn func(key, raw []byte) error) error {
	iter, err := db.NewIterator(t.Prefix)
	if err != nil {
		return err
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()
		if err := fn(key, value); err != nil {
			return err
		}
	}
	return iter.Error()
}

func (t *Table) IterValue(db *store.Store, fn func(raw []byte) error) error {
	iter, err := db.NewIterator(t.Prefix)
	if err != nil {
		return err
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		value := iter.Value()
		if err := fn(value); err != nil {
			return err
		}
	}
	return iter.Error()
}

// find agents etc in farm
func (t *Table) Find(db *store.Store, fHash string, fn func(raw []byte) error) error {
	var buf bytes.Buffer
	buf.Write(t.Prefix)
	buf.Write(store.KeyFromString(fHash))
	kp := buf.Bytes()

	iter, err := db.NewIterator(kp)
	if err != nil {
		return err
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		value := iter.Value()
		if err := fn(value); err != nil {
			return err
		}
	}
	return iter.Error()
}
