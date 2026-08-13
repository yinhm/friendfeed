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

// Reversed Entry index, mirroring the TimelineIndex layout:
//
//	K-> | table | owner uuid | reverse unix ms | entry uuid |
//	V-> empty
//
// The key is deterministic, so re-indexing one entry overwrites the same row.
// Readers reconstruct the canonical Entry key from the key suffix.
func (t *Table) Index(db *store.Store, indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) error {
	k, err := entryIndexKey(indexUUID, oldtime, entryKey)
	if err != nil {
		return err
	}
	return db.Set(k, nil)
}

// indexBatch preserves Index's key encoding and overwrite semantics while
// adding its mutations to the caller's atomic batch.
func (t *Table) indexBatch(batch *pebble.Batch, indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) error {
	k, err := entryIndexKey(indexUUID, oldtime, entryKey)
	if err != nil {
		return err
	}
	return batch.Set(k, nil, nil)
}

func validateCanonicalEntryKey(key store.Key) error {
	if len(key) != Entry.Prefix.Len()+uuid.Size {
		return fmt.Errorf("noncanonical Entry key length %d", len(key))
	}
	if !bytes.Equal(key[:Entry.Prefix.Len()], Entry.Prefix) {
		return fmt.Errorf("noncanonical Entry key prefix %x", key[:Entry.Prefix.Len()])
	}
	if _, err := uuid.FromBytes(key[Entry.Prefix.Len():]); err != nil {
		return fmt.Errorf("noncanonical Entry key UUID: %w", err)
	}
	return nil
}

// EntryIndexKey encodes a direct feed index row: owner UUID, reverse Unix
// millisecond activity time and entry UUID. The value is empty.
func EntryIndexKey(owner, entry uuid.UUID, at time.Time) (store.Key, error) {
	reverse, err := reverseTimelineMillis(at)
	if err != nil {
		return nil, err
	}
	return NewKeyFrom(EntryIndex.Prefix, owner.Bytes(), reverse[:], entry.Bytes()), nil
}

// EntryIndexKeySize is the encoded direct feed index key length:
// table prefix (4) + owner UUID (16) + reverse Unix ms (8) + entry UUID (16).
const EntryIndexKeySize = 4 + uuid.Size + 8 + uuid.Size

// ParseEntryIndexKey decodes a direct feed index key written by EntryIndexKey.
func ParseEntryIndexKey(key store.Key) (owner, entry uuid.UUID, at time.Time, err error) {
	if len(key) != EntryIndexKeySize {
		return owner, entry, at, fmt.Errorf("invalid EntryIndex key length %d", len(key))
	}
	owner, err = uuid.FromBytes(key[4 : 4+uuid.Size])
	if err != nil {
		return owner, entry, at, err
	}
	ms := ^binary.BigEndian.Uint64(key[4+uuid.Size : 4+uuid.Size+8])
	if ms > uint64(^uint64(0)>>1) {
		return owner, entry, at, errors.New("entry index timestamp overflows int64")
	}
	entry, err = uuid.FromBytes(key[4+uuid.Size+8:])
	return owner, entry, time.UnixMilli(int64(ms)).UTC(), err
}

func entryIndexKey(indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) (store.Key, error) {
	if err := validateCanonicalEntryKey(entryKey); err != nil {
		return nil, err
	}
	entryUUID, err := uuid.FromBytes(entryKey[Entry.Prefix.Len():])
	if err != nil {
		return nil, fmt.Errorf("noncanonical Entry key UUID: %w", err)
	}
	return EntryIndexKey(indexUUID, entryUUID, oldtime)
}

func (t *Table) RemoveIndex(db *store.Store, indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) error {
	k, err := entryIndexKey(indexUUID, oldtime, entryKey)
	if err != nil {
		return err
	}
	return db.Delete(k)
}

func (t *Table) removeIndexBatch(batch *pebble.Batch, indexUUID uuid.UUID, oldtime time.Time, entryKey store.Key) error {
	k, err := entryIndexKey(indexUUID, oldtime, entryKey)
	if err != nil {
		return err
	}
	return batch.Delete(k, nil)
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
