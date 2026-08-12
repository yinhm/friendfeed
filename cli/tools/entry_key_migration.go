package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type entryKeyMigrationStats struct {
	scanned   int
	canonical int
	migrated  int
}

type entryKeyMigrationOp struct {
	oldKey store.Key
	newKey store.Key
}

// migrateEntryKeys repairs the short-lived 2021 layout that stored
// TableEntry|UUID-string. It validates the complete table before writing so a
// malformed row or conflicting canonical row is found before any write.
func migrateEntryKeys(db *store.Store, dryRun bool) (entryKeyMigrationStats, error) {
	stats := entryKeyMigrationStats{}
	ops := make([]entryKeyMigrationOp, 0)
	err := model.Entry.Iter(db, func(key, value []byte) error {
		stats.scanned++
		entry := new(pb.Entry)
		if err := proto.Unmarshal(value, entry); err != nil {
			return fmt.Errorf("decode Entry[%x]: %w", key, err)
		}
		entryID, err := uuid.FromString(entry.Id)
		if err != nil {
			return fmt.Errorf("Entry[%x] has invalid Id %q: %w", key, entry.Id, err)
		}
		canonical := model.Entry.PrefixAppend(entryID.Bytes())
		if bytes.Equal(key, canonical) {
			stats.canonical++
			return nil
		}
		if len(key) != model.Entry.Prefix.Len()+36 ||
			!bytes.Equal(key[:model.Entry.Prefix.Len()], model.Entry.Prefix) {
			return fmt.Errorf("Entry[%x] has unsupported noncanonical key", key)
		}
		keyID, err := uuid.FromString(string(key[model.Entry.Prefix.Len():]))
		if err != nil {
			return fmt.Errorf("Entry[%x] has invalid UUID-string key: %w", key, err)
		}
		if keyID != entryID {
			return fmt.Errorf("Entry[%x] key UUID %s disagrees with Id %s", key, keyID, entryID)
		}
		if existing, err := db.Get(canonical); err == nil {
			canonicalEntry := new(pb.Entry)
			if err := proto.Unmarshal(existing, canonicalEntry); err != nil {
				return fmt.Errorf("decode canonical Entry[%x]: %w", canonical, err)
			}
			if !proto.Equal(entry, canonicalEntry) {
				return fmt.Errorf("Entry[%x] conflicts with canonical Entry[%x]", key, canonical)
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("read canonical Entry[%x]: %w", canonical, err)
		}
		stats.migrated++
		ops = append(ops, entryKeyMigrationOp{
			oldKey: append(store.Key(nil), key...), newKey: canonical,
		})
		return nil
	})
	if err != nil || dryRun {
		return stats, err
	}
	const batchSize = 500
	for start := 0; start < len(ops); start += batchSize {
		end := min(start+batchSize, len(ops))
		if err := db.ApplyBatch(func(batch *pebble.Batch) error {
			for _, op := range ops[start:end] {
				value, err := db.Get(op.oldKey)
				if err != nil {
					return fmt.Errorf("read Entry[%x] for migration: %w", op.oldKey, err)
				}
				if err := batch.Set(op.newKey, value, nil); err != nil {
					return err
				}
				if err := batch.Delete(op.oldKey, nil); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return stats, fmt.Errorf("commit Entry key migration: %w", err)
		}
	}
	return stats, nil
}
