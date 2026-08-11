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
	if err := removePreviousEntryIndex(db, nil, indexUUID, oldtime, entryKey); err != nil {
		return err
	}
	return db.Put(entryIndexKey(db, indexUUID, oldtime, entryKey), entryKey)
}

// indexBatch preserves Index's key encoding and duplicate-removal semantics
// while adding its mutations to the caller's atomic batch.
func (t *Table) indexBatch(db *store.Store, batch *pebble.Batch, indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) error {
	if err := removePreviousEntryIndex(db, batch, indexUUID, oldtime, entryKey); err != nil {
		return err
	}
	flakeid := db.TimeTravelReverseId(oldtime)
	k := store.NewUUIDFlakeKey(TableEntryIndex, indexUUID, flakeid)
	return batch.Set(NewKeyFrom(k.Bytes(), entryKey), entryKey, nil)
}

func removePreviousEntryIndex(db *store.Store, batch *pebble.Batch, indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) error {
	flakeID := db.TimeTravelReverseId(oldtime)
	base := store.NewUUIDFlakeKey(TableEntryIndex, indexUUID, flakeID)
	var layout flake.Generator
	uniqueSuffixSize := len(layout.WorkerId) + binary.Size(layout.Sequence)
	timestampPrefix := base.Bytes()[:base.Len()-uniqueSuffixSize]
	_, err := db.ForwardScan(timestampPrefix, func(_ int, key, value []byte) error {
		if !bytes.Equal(value, entryKey) {
			return nil
		}
		if batch != nil {
			return batch.Delete(key, nil)
		}
		return db.Delete(key)
	})
	if err != nil {
		return fmt.Errorf("remove previous entry index: %w", err)
	}
	return nil
}

func entryIndexKey(db *store.Store, indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) store.Key {
	flakeID := db.TimeTravelReverseId(oldtime)
	base := store.NewUUIDFlakeKey(TableEntryIndex, indexUUID, flakeID)
	return NewKeyFrom(base.Bytes(), entryKey)
}

func (t *Table) RemoveIndex(db *store.Store, indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) error {
	return db.Delete(entryIndexKey(db, indexUUID, oldtime, entryKey))
}

func (t *Table) removeIndexBatch(db *store.Store, batch *pebble.Batch, indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) error {
	return batch.Delete(entryIndexKey(db, indexUUID, oldtime, entryKey), nil)
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
