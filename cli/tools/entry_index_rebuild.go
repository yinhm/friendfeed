package main

import (
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
	entries  int
	direct   int
	timeline int
	removed  int
}

type entryIndexRebuildRecord struct {
	key      store.Key
	date     time.Time
	direct   []uuid.UUID
	timeline []uuid.UUID
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

	followers := make(map[uuid.UUID][]uuid.UUID)
	records := make([]entryIndexRebuildRecord, 0)
	loadFollowers := func(feed uuid.UUID) ([]uuid.UUID, error) {
		if cached, ok := followers[feed]; ok {
			return cached, nil
		}
		list := make([]uuid.UUID, 0)
		prefix := model.NewPrefixKeyFrom(model.TableFollower, feed.Bytes())
		_, err := db.ForwardScan(prefix, func(_ int, key, _ []byte) error {
			followerKey := model.ParseFollowerKey(key)
			follower, err := uuid.FromBytes(followerKey)
			if err != nil {
				return err
			}
			list = append(list, follower)
			return nil
		})
		followers[feed] = list
		return list, err
	}

	err := model.Entry.Iter(db, func(key, raw []byte) error {
		if options.maxLimit > 0 && stats.entries >= options.maxLimit {
			return &store.Error{Msg: "entry index rebuild limit reached", Code: store.StopIteration}
		}
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
		date, err := time.Parse(time.RFC3339, entry.Date)
		if err != nil {
			return fmt.Errorf("entry %q date: %w", entry.Id, err)
		}
		entryKey := store.Key(key)
		stats.entries++
		directTargets := []uuid.UUID{author}
		if feed != author {
			directTargets = append(directTargets, feed)
		}
		stats.direct += len(directTargets)
		timelineTargets := []uuid.UUID{model.TimelineUUID(author)}
		feedFollowers, err := loadFollowers(feed)
		if err != nil {
			return err
		}
		for _, follower := range feedFollowers {
			timelineTargets = append(timelineTargets, model.TimelineUUID(follower))
		}
		stats.timeline += len(timelineTargets)
		records = append(records, entryIndexRebuildRecord{
			key: append(store.Key(nil), entryKey...), date: date,
			direct: directTargets, timeline: timelineTargets,
		})
		return nil
	})
	if err != nil || options.dryRun {
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
		for _, target := range record.timeline {
			if err := model.EntryIndex.Index(db, target, record.date, record.key); err != nil {
				return stats, err
			}
		}
	}
	return stats, nil
}
