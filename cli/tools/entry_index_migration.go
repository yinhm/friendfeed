package main

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/store/flake"
)

// EntryIndex key layouts, oldest first:
//
//	legacy   T(4) | owner(16) | reverse Flake(16)              value = canonical Entry key(20)
//	previous T(4) | owner(16) | reverse Flake(16) | entry key(20) value = canonical Entry key(20)
//	current  T(4) | owner(16) | reverse ms(8) | entry UUID(16)  value = empty
const (
	legacyEntryIndexKeySize   = 4 + uuid.Size + 16
	previousEntryIndexKeySize = legacyEntryIndexKeySize + 4 + uuid.Size
)

type entryIndexMigrationStats struct {
	scanned  int
	migrated int
	current  int
}

type entryIndexMigrationOp struct {
	oldKey []byte
	newKey []byte
}

func migrateEntryIndex(db *store.Store, dryRun bool, maxLimit int) (entryIndexMigrationStats, error) {
	stats := entryIndexMigrationStats{}
	const batchSize = 500
	ops := make([]entryIndexMigrationOp, 0, batchSize)
	flush := func() error {
		if len(ops) == 0 {
			return nil
		}
		if err := db.ApplyBatch(func(batch *pebble.Batch) error {
			for _, op := range ops {
				if err := batch.Set(op.newKey, nil, nil); err != nil {
					return err
				}
				if err := batch.Delete(op.oldKey, nil); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("commit EntryIndex migration batch: %w", err)
		}
		ops = ops[:0]
		return nil
	}
	_, err := db.ForwardScan(model.EntryIndex.Prefix, func(_ int, key, value []byte) error {
		if maxLimit > 0 && stats.scanned >= maxLimit {
			return &store.Error{Msg: "entry index migration limit reached", Code: store.StopIteration}
		}
		stats.scanned++
		switch len(key) {
		case model.EntryIndexKeySize:
			stats.current++
		case legacyEntryIndexKeySize, previousEntryIndexKeySize:
			newKey, err := transformEntryIndexKey(key, value)
			if err != nil {
				return err
			}
			stats.migrated++
			if !dryRun {
				// key borrows the scan buffer; retain only the current bounded
				// batch and release it immediately after commit.
				ops = append(ops, entryIndexMigrationOp{
					oldKey: append([]byte(nil), key...),
					newKey: newKey,
				})
				if len(ops) == batchSize {
					return flush()
				}
			}
		default:
			return fmt.Errorf("EntryIndex[%x] has unsupported key length %d", key, len(key))
		}
		return nil
	})
	if err != nil {
		return stats, err
	}
	if dryRun {
		return stats, nil
	}
	return stats, flush()
}

// transformEntryIndexKey converts a legacy or previous EntryIndex row to the
// current layout. The legacy reverse Flake stores MaxTime - entry time
// truncated to whole seconds, so the entry time is recoverable from the key
// alone; the entry UUID comes from the value (legacy) or the key suffix
// (previous). Duplicate legacy rows collapse onto one current key.
func transformEntryIndexKey(key, value []byte) (store.Key, error) {
	owner, err := uuid.FromBytes(key[4 : 4+uuid.Size])
	if err != nil {
		return nil, fmt.Errorf("EntryIndex[%x] owner UUID: %w", key, err)
	}
	reverseFlakeMs := binary.BigEndian.Uint64(key[4+uuid.Size : 4+uuid.Size+8])
	forwardMs := flake.MaxTime.UnixMilli() - int64(reverseFlakeMs)
	if forwardMs < 0 {
		return nil, fmt.Errorf("EntryIndex[%x] has negative entry time", key)
	}

	var entryRaw []byte
	switch len(key) {
	case legacyEntryIndexKeySize:
		if len(value) != 4+uuid.Size {
			return nil, fmt.Errorf("legacy EntryIndex[%x] has invalid entry key length %d", key, len(value))
		}
		entryRaw = value[4:]
	default: // previousEntryIndexKeySize
		entryRaw = key[4+uuid.Size+16+4:]
	}
	entry, err := uuid.FromBytes(entryRaw)
	if err != nil {
		return nil, fmt.Errorf("EntryIndex[%x] entry UUID: %w", key, err)
	}
	return model.EntryIndexKey(owner, entry, time.UnixMilli(forwardMs).UTC())
}
