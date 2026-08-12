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
	timelineIndexes      int
	timelinePositions    int
	timelineMissingEntry int
	timelineMissingPos   int
	timelineMissingIndex int
	timelineDuplicates   int
	timelineTimeMismatch int
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
	followerCounts := make(map[uuid.UUID]int)
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
		followerCounts[feed]++
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
	for _, count := range followerCounts {
		stats.maxFollowers = max(stats.maxFollowers, count)
	}

	actualIndexes := make(map[string]struct{})
	if err := model.EntryIndex.Iter(db, func(key, value []byte) error {
		if len(key) < 4+uuid.Size+16 {
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
	timelineRows := make(map[string]time.Time)
	if err := model.TimelineIndex.Iter(db, func(key, _ []byte) error {
		viewer, entry, activity, err := model.ParseTimelineIndexKey(key)
		if err != nil {
			return err
		}
		pair := viewer.String() + "/" + entry.String()
		if _, exists := timelineRows[pair]; exists {
			stats.timelineDuplicates++
		}
		timelineRows[pair] = activity
		if _, ok := entries[hex.EncodeToString(model.Entry.PrefixAppend(entry.Bytes()))]; !ok {
			stats.timelineMissingEntry++
		}
		stats.timelineIndexes++
		return nil
	}); err != nil {
		return stats, err
	}
	positions := make(map[string]time.Time)
	if err := model.TimelinePosition.Iter(db, func(key, value []byte) error {
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid TimelinePosition key length %d", len(key))
		}
		viewer, _ := uuid.FromBytes(key[4 : 4+uuid.Size])
		entry, _ := uuid.FromBytes(key[4+uuid.Size:])
		activity, err := model.TimelinePositionTime(db, viewer, entry)
		if err != nil {
			return err
		}
		positions[viewer.String()+"/"+entry.String()] = activity
		stats.timelinePositions++
		return nil
	}); err != nil {
		return stats, err
	}
	for pair, activity := range timelineRows {
		position, ok := positions[pair]
		if !ok {
			stats.timelineMissingPos++
		} else if !position.Equal(activity) {
			stats.timelineTimeMismatch++
		}
	}
	for pair := range positions {
		if _, ok := timelineRows[pair]; !ok {
			stats.timelineMissingIndex++
		}
	}
	return stats, nil
}

func writeStoreAudit(out io.Writer, stats storeAuditStats) {
	fmt.Fprintf(out, "entries=%d entry_indexes=%d missing_direct=%d orphan_indexes=%d\n",
		stats.entries, stats.entryIndexes, stats.missingDirectIndexes, stats.orphanIndexes)
	fmt.Fprintf(out, "timeline_indexes=%d timeline_positions=%d missing_entry=%d missing_position=%d missing_index=%d duplicates=%d timestamp_mismatch=%d\n",
		stats.timelineIndexes, stats.timelinePositions, stats.timelineMissingEntry, stats.timelineMissingPos,
		stats.timelineMissingIndex, stats.timelineDuplicates, stats.timelineTimeMismatch)
	fmt.Fprintf(out, "same_second_groups=%d same_second_entries=%d\n", stats.sameSecondGroups, stats.sameSecondEntries)
	fmt.Fprintf(out, "follow=%d follower=%d missing_follower=%d missing_follow=%d max_followers=%d\n",
		stats.followEdges, stats.followerEdges, stats.missingFollowerEdges, stats.missingFollowEdges, stats.maxFollowers)
}
