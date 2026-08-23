package model

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/store"
)

const groupIndexKeySize = 4 + 8 + uuid.Size

func GroupIndexKey(group uuid.UUID, activity time.Time) (store.Key, error) {
	if group == uuid.Nil {
		return nil, errors.New("Group UUID is zero")
	}
	ms := activity.UTC().UnixMilli()
	if ms < 0 {
		return nil, fmt.Errorf("Group activity before Unix epoch: %s", activity)
	}
	var reverse [8]byte
	binary.BigEndian.PutUint64(reverse[:], ^uint64(ms))
	return NewKeyFrom(GroupIndex.Prefix, reverse[:], group.Bytes()), nil
}

func ParseGroupIndexKey(key store.Key) (uuid.UUID, time.Time, error) {
	if len(key) != groupIndexKeySize || !bytes.Equal(key[:4], GroupIndex.Prefix) {
		return uuid.Nil, time.Time{}, fmt.Errorf("invalid GroupIndex key length or prefix")
	}
	ms := ^binary.BigEndian.Uint64(key[4:12])
	if ms > uint64(^uint64(0)>>1) {
		return uuid.Nil, time.Time{}, errors.New("GroupIndex timestamp overflows int64")
	}
	group, err := uuid.FromBytes(key[12:])
	if err != nil || group == uuid.Nil {
		return uuid.Nil, time.Time{}, errors.New("GroupIndex UUID is invalid")
	}
	return group, time.UnixMilli(int64(ms)).UTC(), nil
}

// StageMoveGroupIndex replaces one Group's activity position in the caller's
// batch. The index intentionally has no position table: Group count is small,
// so a bounded-memory directory scan keeps the persisted design minimal.
func StageMoveGroupIndex(db *store.Store, batch *pebble.Batch, group uuid.UUID, activity time.Time) error {
	if db == nil || batch == nil {
		return errors.New("store and batch are required")
	}
	activity = activity.UTC().Truncate(time.Millisecond)
	newKey, err := GroupIndexKey(group, activity)
	if err != nil {
		return err
	}
	iter, err := db.NewIterator(GroupIndex.Prefix)
	if err != nil {
		return err
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		indexedGroup, oldActivity, err := ParseGroupIndexKey(iter.UnsafeKey())
		if err != nil {
			return err
		}
		if indexedGroup != group {
			continue
		}
		if !activity.After(oldActivity) {
			return nil
		}
		if err := batch.Delete(iter.Key(), nil); err != nil {
			return err
		}
		break
	}
	if err := iter.Error(); err != nil {
		return err
	}
	return batch.Set(newKey, nil, nil)
}

func StageCreateGroupIndex(batch *pebble.Batch, group uuid.UUID, createdAt time.Time) error {
	if batch == nil {
		return errors.New("batch is required")
	}
	key, err := GroupIndexKey(group, createdAt.UTC().Truncate(time.Millisecond))
	if err != nil {
		return err
	}
	return batch.Set(key, nil, nil)
}
