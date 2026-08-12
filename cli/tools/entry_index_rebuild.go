package main

import (
	"bytes"
	"fmt"
	"sort"
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
	stats := entryIndexRebuildStats{}
	var selected uuid.UUID
	if options.user != "" {
		profile, err := model.GetProfileFromUserId(db, options.user)
		if err != nil {
			return stats, fmt.Errorf("resolve profile %q: %w", options.user, err)
		}
		selected, err = uuid.FromString(profile.Uuid)
		if err != nil {
			return stats, err
		}
	}

	records := make([]entryIndexRebuildRecord, 0)

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
		if options.maxLimit > 0 && stats.entries >= options.maxLimit {
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
		stats.entries++
		directTargets := []uuid.UUID{author}
		if feed != author {
			directTargets = append(directTargets, feed)
		}
		stats.direct += len(directTargets)
		records = append(records, entryIndexRebuildRecord{
			key: append(store.Key(nil), entryKey...), date: date,
			direct: directTargets,
		})
		return nil
	})
	if err != nil || options.dryRun {
		if err == nil && selected == uuid.Nil {
			err = compareEntryIndexes(db, records, &stats)
		}
		return stats, err
	}

	if selected == uuid.Nil {
		var keys [][]byte
		if _, err := db.ForwardScan(model.EntryIndex.Prefix, func(_ int, key, _ []byte) error {
			keys = append(keys, append([]byte(nil), key...))
			return nil
		}); err != nil {
			return stats, err
		}
		const batchSize = 500
		for start := 0; start < len(keys); start += batchSize {
			end := min(start+batchSize, len(keys))
			if err := db.ApplyBatch(func(batch *pebble.Batch) error {
				for _, key := range keys[start:end] {
					if err := batch.Delete(key, nil); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return stats, fmt.Errorf("clear EntryIndex: %w", err)
			}
		}
		stats.removed = len(keys)
	}
	for _, record := range records {
		for _, target := range record.direct {
			if err := model.EntryIndex.Index(db, target, record.date, record.key); err != nil {
				return stats, err
			}
		}
	}
	return stats, nil
}

func compareEntryIndexes(db *store.Store, records []entryIndexRebuildRecord, stats *entryIndexRebuildStats) error {
	type expectedIndex struct {
		key   store.Key
		value store.Key
	}
	expected := make(map[uuid.UUID][]expectedIndex)
	for _, record := range records {
		for _, target := range record.direct {
			flakeID := db.TimeTravelReverseId(record.date)
			base := store.NewUUIDFlakeKey(model.TableEntryIndex, target, flakeID)
			key := model.NewKeyFrom(base.Bytes(), record.key)
			expected[target] = append(expected[target], expectedIndex{key: key, value: record.key})
		}
	}
	for target, indexes := range expected {
		sort.Slice(indexes, func(i, j int) bool { return bytes.Compare(indexes[i].key, indexes[j].key) < 0 })
		want := make([][]byte, len(indexes))
		for i, index := range indexes {
			want[i] = index.value
		}

		actual := make([][]byte, 0, len(want))
		seen := make(map[string]struct{})
		prefix := store.NewUUIDKey(model.TableEntryIndex, target).Bytes()
		if _, err := db.ForwardScan(prefix, func(_ int, _, value []byte) error {
			value = append([]byte(nil), value...)
			if _, ok := seen[string(value)]; ok {
				stats.duplicateIndexes++
			}
			seen[string(value)] = struct{}{}
			actual = append(actual, value)
			return nil
		}); err != nil {
			return fmt.Errorf("compare EntryIndex for %s: %w", target, err)
		}
		stats.feedsChecked++
		if len(actual) != len(want) {
			stats.feedsMismatched++
			continue
		}
		for i := range want {
			if !bytes.Equal(actual[i], want[i]) {
				stats.feedsMismatched++
				break
			}
		}
	}
	return nil
}
