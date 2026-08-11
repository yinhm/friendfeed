package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type storeAuditStats struct {
	entries              int
	entryIndexes         int
	missingDirectIndexes int
	orphanIndexes        int
	missingTimeline      int
	sameSecondGroups     int
	sameSecondEntries    int
	followEdges          int
	followerEdges        int
	missingFollowerEdges int
	missingFollowEdges   int
	maxFollowers         int
}

type auditEntry struct {
	key    string
	author uuid.UUID
	feed   uuid.UUID
	date   time.Time
}

func auditPair(owner uuid.UUID, entryKey string) string {
	return owner.String() + "/" + entryKey
}

func auditStore(db *store.Store) (storeAuditStats, error) {
	stats := storeAuditStats{}
	entries := make(map[string]auditEntry)
	directExpected := make(map[string]struct{})
	sameSecond := make(map[string]int)

	if err := model.Entry.Iter(db, func(key, raw []byte) error {
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
		date, err := time.Parse(time.RFC3339, entry.Date)
		if err != nil {
			return fmt.Errorf("entry %q date: %w", entry.Id, err)
		}
		entryKey := hex.EncodeToString(key)
		entries[entryKey] = auditEntry{key: entryKey, author: author, feed: feed, date: date}
		directExpected[auditPair(author, entryKey)] = struct{}{}
		directExpected[auditPair(feed, entryKey)] = struct{}{}
		sameSecond[feed.String()+"/"+date.UTC().Truncate(time.Second).Format(time.RFC3339)]++
		stats.entries++
		return nil
	}); err != nil {
		return stats, err
	}
	for _, count := range sameSecond {
		if count > 1 {
			stats.sameSecondGroups++
			stats.sameSecondEntries += count
		}
	}

	follows := make(map[string]struct{})
	followers := make(map[string]struct{})
	followerUUIDs := make(map[uuid.UUID][]uuid.UUID)
	if err := model.Follow.Iter(db, func(key, _ []byte) error {
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid Follow key length %d", len(key))
		}
		follower, _ := uuid.FromBytes(key[4 : 4+uuid.Size])
		feed, _ := uuid.FromBytes(key[4+uuid.Size:])
		follows[follower.String()+"/"+feed.String()] = struct{}{}
		stats.followEdges++
		return nil
	}); err != nil {
		return stats, err
	}
	if err := model.Follower.Iter(db, func(key, _ []byte) error {
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid Follower key length %d", len(key))
		}
		feed, _ := uuid.FromBytes(key[4 : 4+uuid.Size])
		follower, _ := uuid.FromBytes(key[4+uuid.Size:])
		followers[follower.String()+"/"+feed.String()] = struct{}{}
		followerUUIDs[feed] = append(followerUUIDs[feed], follower)
		stats.followerEdges++
		return nil
	}); err != nil {
		return stats, err
	}
	for edge := range follows {
		if _, ok := followers[edge]; !ok {
			stats.missingFollowerEdges++
		}
	}
	for edge := range followers {
		if _, ok := follows[edge]; !ok {
			stats.missingFollowEdges++
		}
	}
	for _, list := range followerUUIDs {
		if len(list) > stats.maxFollowers {
			stats.maxFollowers = len(list)
		}
	}

	actualIndexes := make(map[string]struct{})
	if err := model.EntryIndex.Iter(db, func(key, value []byte) error {
		if len(key) != 4+uuid.Size+16 {
			return fmt.Errorf("invalid EntryIndex key length %d", len(key))
		}
		owner, _ := uuid.FromBytes(key[4 : 4+uuid.Size])
		entryKey := hex.EncodeToString(value)
		actualIndexes[auditPair(owner, entryKey)] = struct{}{}
		if _, ok := entries[entryKey]; !ok {
			stats.orphanIndexes++
		}
		stats.entryIndexes++
		return nil
	}); err != nil {
		return stats, err
	}
	for pair := range directExpected {
		if _, ok := actualIndexes[pair]; !ok {
			stats.missingDirectIndexes++
		}
	}
	for _, entry := range entries {
		expected := []uuid.UUID{model.TimelineUUID(entry.author)}
		for _, follower := range followerUUIDs[entry.feed] {
			expected = append(expected, model.TimelineUUID(follower))
		}
		for _, timeline := range expected {
			if _, ok := actualIndexes[auditPair(timeline, entry.key)]; !ok {
				stats.missingTimeline++
			}
		}
	}
	return stats, nil
}

func writeStoreAudit(out io.Writer, stats storeAuditStats) {
	fmt.Fprintf(out, "entries=%d entry_indexes=%d missing_direct=%d orphan_indexes=%d missing_timeline=%d\n",
		stats.entries, stats.entryIndexes, stats.missingDirectIndexes, stats.orphanIndexes, stats.missingTimeline)
	fmt.Fprintf(out, "same_second_groups=%d same_second_entries=%d\n", stats.sameSecondGroups, stats.sameSecondEntries)
	fmt.Fprintf(out, "follow=%d follower=%d missing_follower=%d missing_follow=%d max_followers=%d\n",
		stats.followEdges, stats.followerEdges, stats.missingFollowerEdges, stats.missingFollowEdges, stats.maxFollowers)
}
