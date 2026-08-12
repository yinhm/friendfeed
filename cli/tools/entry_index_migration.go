package main

import (
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/store"
)

type entryIndexMigrationStats struct {
	scanned  int
	migrated int
	current  int
}

type entryIndexMigrationOp struct {
	oldKey []byte
	newKey []byte
	value  []byte
}

func migrateEntryIndex(db *store.Store, dryRun bool, maxLimit int) (entryIndexMigrationStats, error) {
	stats := entryIndexMigrationStats{}
	ops := make([]entryIndexMigrationOp, 0)
	_, err := db.ForwardScan(model.EntryIndex.Prefix, func(_ int, key, value []byte) error {
		if maxLimit > 0 && stats.scanned >= maxLimit {
			return &store.Error{Msg: "entry index migration limit reached", Code: store.StopIteration}
		}
		stats.scanned++
		const legacyKeySize = 4 + uuid.Size + 16
		switch len(key) {
		case legacyKeySize:
			if len(value) != 4+uuid.Size {
				return fmt.Errorf("legacy EntryIndex[%x] has invalid entry key length %d", key, len(value))
			}
			stats.migrated++
			if !dryRun {
				newKey := make([]byte, 0, len(key)+len(value))
				newKey = append(newKey, key...)
				newKey = append(newKey, value...)
				// key/value borrow the scan buffer and are replayed after the
				// scan completes; copy them before retaining in ops.
				ops = append(ops, entryIndexMigrationOp{
					oldKey: append([]byte(nil), key...),
					newKey: newKey,
					value:  append([]byte(nil), value...),
				})
			}
		case legacyKeySize + 4 + uuid.Size:
			stats.current++
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
	const batchSize = 500
	for start := 0; start < len(ops); start += batchSize {
		end := min(start+batchSize, len(ops))
		if err := db.ApplyBatch(func(batch *pebble.Batch) error {
			for _, op := range ops[start:end] {
				if err := batch.Set(op.newKey, op.value, nil); err != nil {
					return err
				}
				if err := batch.Delete(op.oldKey, nil); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return stats, fmt.Errorf("commit EntryIndex migration batch: %w", err)
		}
	}
	return stats, nil
}
