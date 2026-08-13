package main

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type entryIndexRebuildOptions struct {
	user     string
	maxLimit int
	dryRun   bool
}

type entryIndexRebuildStats struct {
	entries          int
	direct           int
	removed          int
	feedsChecked     int
	feedsMismatched  int
	duplicateIndexes int
}

type entryIndexRebuildRecord struct {
	key    store.Key
	date   time.Time
	direct []uuid.UUID
}

func rebuildEntryIndexes(db *store.Store, options entryIndexRebuildOptions) (entryIndexRebuildStats, error) {
	selected, err := selectedEntryIndexProfile(db, options.user)
	if err != nil {
		return entryIndexRebuildStats{}, err
	}

	// Validate the complete selected source before a full rebuild clears any
	// derived rows. Records are deliberately not retained: production databases
	// contain millions of entries and the old all-record slice exhausted memory.
	stats, err := scanEntryIndexSources(db, selected, options.maxLimit, nil)
	if err != nil {
		return stats, err
	}
	if options.dryRun {
		if selected == uuid.Nil {
			err = compareEntryIndexes(db, selected, options.maxLimit, &stats)
		}
		return stats, err
	}

	if selected == uuid.Nil {
		stats.removed, err = clearEntryIndexes(db)
		if err != nil {
			return stats, fmt.Errorf("clear EntryIndex: %w", err)
		}
	}
	_, err = scanEntryIndexSources(db, selected, options.maxLimit, func(record entryIndexRebuildRecord) error {
		for _, target := range record.direct {
			if err := model.EntryIndex.Index(db, target, record.date, record.key); err != nil {
				return err
			}
		}
		return nil
	})
	return stats, err
}

func clearEntryIndexes(db *store.Store) (int, error) {
	removed, err := db.ForwardScan(model.EntryIndex.Prefix, func(_ int, _, _ []byte) error { return nil })
	if err != nil || removed == 0 {
		return removed, err
	}
	upper := store.KeyUpperBound(model.EntryIndex.Prefix)
	if upper == nil {
		return 0, errors.New("EntryIndex prefix has no upper bound")
	}
	return removed, db.ApplyBatch(func(batch *pebble.Batch) error {
		return batch.DeleteRange(model.EntryIndex.Prefix, upper, nil)
	})
}

func selectedEntryIndexProfile(db *store.Store, user string) (uuid.UUID, error) {
	if user == "" {
		return uuid.Nil, nil
	}
	profile, err := model.GetProfileFromUserId(db, user)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve profile %q: %w", user, err)
	}
	selected, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return uuid.Nil, fmt.Errorf("profile %q UUID: %w", user, err)
	}
	return selected, nil
}

func scanEntryIndexSources(db *store.Store, selected uuid.UUID, maxLimit int, fn func(entryIndexRebuildRecord) error) (entryIndexRebuildStats, error) {
	stats := entryIndexRebuildStats{}
	err := model.Entry.Iter(db, func(key, raw []byte) error {
		entry := new(pb.Entry)
		if err := proto.Unmarshal(raw, entry); err != nil {
			return fmt.Errorf("decode Entry[%x]: %w", key, err)
		}
		author, err := uuid.FromString(entry.ProfileUuid)
		if err != nil {
			return fmt.Errorf("entry %q author UUID: %w", entry.Id, err)
		}
		feed := author
		if entry.FeedUuid != "" {
			feed, err = uuid.FromString(entry.FeedUuid)
			if err != nil {
				return fmt.Errorf("entry %q feed UUID: %w", entry.Id, err)
			}
		}
		if selected != uuid.Nil && author != selected && feed != selected {
			return nil
		}
		if maxLimit > 0 && stats.entries >= maxLimit {
			return nil
		}
		date, err := time.Parse(time.RFC3339, entry.Date)
		if err != nil {
			return fmt.Errorf("entry %q date: %w", entry.Id, err)
		}
		entryID, err := uuid.FromString(entry.Id)
		if err != nil {
			return fmt.Errorf("entry %q UUID: %w", entry.Id, err)
		}
		entryKey := model.Entry.PrefixAppend(entryID.Bytes())
		if !bytes.Equal(key, entryKey) {
			return fmt.Errorf("entry %q has noncanonical key %x; run migrate_entry_keys", entry.Id, key)
		}
		direct := []uuid.UUID{author}
		if feed != author {
			direct = append(direct, feed)
		}
		stats.entries++
		stats.direct += len(direct)
		if fn != nil {
			if err := fn(entryIndexRebuildRecord{key: entryKey, date: date, direct: direct}); err != nil {
				return err
			}
		}
		return nil
	})
	return stats, err
}

func expectedEntryIndexKey(target uuid.UUID, date time.Time, entryKey store.Key) (store.Key, error) {
	entryID, err := uuid.FromBytes(entryKey[model.Entry.Prefix.Len():])
	if err != nil {
		return nil, err
	}
	return model.EntryIndexKey(target, entryID, date)
}

// compareEntryIndexes validates both directions without retaining every entry
// or index row. Memory is bounded by the number of feeds, not database rows.
func compareEntryIndexes(db *store.Store, selected uuid.UUID, maxLimit int, stats *entryIndexRebuildStats) error {
	mismatched := make(map[uuid.UUID]bool)
	checked := make(map[uuid.UUID]struct{})
	_, err := scanEntryIndexSources(db, selected, maxLimit, func(record entryIndexRebuildRecord) error {
		for _, target := range record.direct {
			checked[target] = struct{}{}
			expected, err := expectedEntryIndexKey(target, record.date, record.key)
			if err != nil {
				return err
			}
			exists, err := db.Exists(expected)
			if err != nil {
				return err
			}
			if !exists {
				mismatched[target] = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	_, err = db.ForwardScan(model.EntryIndex.Prefix, func(_ int, key, value []byte) error {
		if len(key) < model.EntryIndex.Prefix.Len()+uuid.Size {
			return fmt.Errorf("invalid EntryIndex key length %d", len(key))
		}
		target, err := uuid.FromBytes(key[model.EntryIndex.Prefix.Len() : model.EntryIndex.Prefix.Len()+uuid.Size])
		if err != nil {
			return err
		}
		if _, expectedFeed := checked[target]; !expectedFeed {
			// Full orphan accounting belongs to audit_store. This comparison
			// reports whether each source-derived feed can be rebuilt exactly.
			return nil
		}
		_, entryID, _, err := model.ParseEntryIndexKey(key)
		if err != nil {
			mismatched[target] = true
			return nil
		}
		entry := new(pb.Entry)
		if err := model.Entry.Get(db, entryID.Bytes(), entry); err != nil {
			if errors.Is(err, model.ErrNotFound) {
				mismatched[target] = true
				return nil
			}
			return err
		}
		author, err := uuid.FromString(entry.ProfileUuid)
		if err != nil {
			return err
		}
		feed := author
		if entry.FeedUuid != "" {
			feed, err = uuid.FromString(entry.FeedUuid)
			if err != nil {
				return err
			}
		}
		if target != author && target != feed {
			mismatched[target] = true
			return nil
		}
		date, err := time.Parse(time.RFC3339, entry.Date)
		if err != nil {
			return err
		}
		expected, err := model.EntryIndexKey(target, entryID, date)
		if err != nil {
			return err
		}
		if !bytes.Equal(key, expected) {
			mismatched[target] = true
			stats.duplicateIndexes++
		}
		return nil
	})
	if err != nil {
		return err
	}
	stats.feedsChecked = len(checked)
	stats.feedsMismatched = len(mismatched)
	return nil
}
