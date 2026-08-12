package main

import (
	"bytes"
	"encoding/binary"
	"errors"
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
	noncanonicalEntries  int
	entryKeyIDMismatches int
	entryIndexes         int
	noncanonicalIndexes  int
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

func auditStore(db *store.Store) (storeAuditStats, error) {
	stats := storeAuditStats{}

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
		entryID, idErr := uuid.FromString(entry.Id)
		if idErr != nil {
			stats.entryKeyIDMismatches++
			return nil
		}
		canonicalKey := model.Entry.PrefixAppend(entryID.Bytes())
		if !bytes.Equal(key, canonicalKey) {
			stats.noncanonicalEntries++
			if len(key) != model.Entry.Prefix.Len()+36 {
				stats.entryKeyIDMismatches++
			} else if keyID, err := uuid.FromString(string(key[model.Entry.Prefix.Len():])); err != nil || keyID != entryID {
				stats.entryKeyIDMismatches++
			}
		}
		owners := []uuid.UUID{author}
		if feed != author {
			owners = append(owners, feed)
		}
		for _, owner := range owners {
			value, err := db.Get(expectedEntryIndexKey(db, owner, date, canonicalKey))
			if errors.Is(err, store.ErrNotFound) || (err == nil && !bytes.Equal(value, canonicalKey)) {
				stats.missingDirectIndexes++
				continue
			}
			if err != nil {
				return err
			}
		}
		stats.entries++
		return nil
	}); err != nil {
		return stats, err
	}

	if err := model.Follow.Iter(db, func(key, _ []byte) error {
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid Follow key length %d", len(key))
		}
		follower, _ := uuid.FromBytes(key[4 : 4+uuid.Size])
		feed, _ := uuid.FromBytes(key[4+uuid.Size:])
		counterpart := model.NewKeyFrom(model.Follower.Prefix, feed.Bytes(), follower.Bytes())
		exists, err := db.Exists(counterpart)
		if err != nil {
			return err
		}
		if !exists {
			stats.missingFollowerEdges++
		}
		stats.followEdges++
		return nil
	}); err != nil {
		return stats, err
	}
	var followerFeed uuid.UUID
	followerCount := 0
	finishFollowerFeed := func() {
		stats.maxFollowers = max(stats.maxFollowers, followerCount)
	}
	if err := model.Follower.Iter(db, func(key, _ []byte) error {
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid Follower key length %d", len(key))
		}
		feed, _ := uuid.FromBytes(key[4 : 4+uuid.Size])
		follower, _ := uuid.FromBytes(key[4+uuid.Size:])
		if followerCount > 0 && feed != followerFeed {
			finishFollowerFeed()
			followerCount = 0
		}
		followerFeed = feed
		followerCount++
		counterpart := model.NewKeyFrom(model.Follow.Prefix, follower.Bytes(), feed.Bytes())
		exists, err := db.Exists(counterpart)
		if err != nil {
			return err
		}
		if !exists {
			stats.missingFollowEdges++
		}
		stats.followerEdges++
		return nil
	}); err != nil {
		return stats, err
	}
	finishFollowerFeed()

	var groupOwner uuid.UUID
	var groupSecond int64
	groupCount := 0
	finishSameSecond := func() {
		if groupCount > 1 {
			stats.sameSecondGroups++
			stats.sameSecondEntries += groupCount
		}
	}
	if err := model.EntryIndex.Iter(db, func(key, value []byte) error {
		if len(key) < 4+uuid.Size+16 {
			return fmt.Errorf("invalid EntryIndex key length %d", len(key))
		}
		owner, _ := uuid.FromBytes(key[4 : 4+uuid.Size])
		canonical, err := canonicalEntryKeyFromIndexValue(value)
		if err != nil {
			stats.noncanonicalIndexes++
			stats.entryIndexes++
			return nil
		}
		entry := new(pb.Entry)
		if err := model.Entry.Get(db, canonical[model.Entry.Prefix.Len():], entry); err != nil {
			if errors.Is(err, model.ErrNotFound) {
				stats.orphanIndexes++
				stats.entryIndexes++
				return nil
			}
			return err
		}
		entryID, err := uuid.FromString(entry.Id)
		if err != nil || !bytes.Equal(canonical, model.Entry.PrefixAppend(entryID.Bytes())) {
			stats.orphanIndexes++
			stats.entryIndexes++
			return nil
		}
		feed, err := uuid.FromString(entry.ProfileUuid)
		if err != nil {
			return err
		}
		if entry.FeedUuid != "" {
			feed, err = uuid.FromString(entry.FeedUuid)
			if err != nil {
				return err
			}
		}
		if owner == feed {
			date, err := time.Parse(time.RFC3339, entry.Date)
			if err != nil {
				return err
			}
			second := date.UTC().Unix()
			if groupCount > 0 && (owner != groupOwner || second != groupSecond) {
				finishSameSecond()
				groupCount = 0
			}
			groupOwner, groupSecond = owner, second
			groupCount++
		}
		stats.entryIndexes++
		return nil
	}); err != nil {
		return stats, err
	}
	finishSameSecond()

	// Keep only drifted pairs. Healthy timeline cardinality does not affect
	// memory; this small exception distinguishes a timestamp mismatch from a
	// wholly missing index during the reverse Position scan.
	timelineMismatchedPairs := make(map[[2]uuid.UUID]struct{})
	if err := model.TimelineIndex.Iter(db, func(key, _ []byte) error {
		viewer, entry, activity, err := model.ParseTimelineIndexKey(key)
		if err != nil {
			return err
		}
		if _, err := db.Get(model.Entry.PrefixAppend(entry.Bytes())); errors.Is(err, store.ErrNotFound) {
			stats.timelineMissingEntry++
		} else if err != nil {
			return err
		}
		position, err := model.TimelinePositionTime(db, viewer, entry)
		if errors.Is(err, store.ErrNotFound) {
			stats.timelineMissingPos++
		} else if err != nil {
			return err
		} else if !position.Equal(activity) {
			timelineMismatchedPairs[[2]uuid.UUID{viewer, entry}] = struct{}{}
			canonical, err := model.TimelineIndexKey(viewer, entry, position)
			if err != nil {
				return err
			}
			exists, err := db.Exists(canonical)
			if err != nil {
				return err
			}
			if exists {
				stats.timelineDuplicates++
			} else {
				stats.timelineTimeMismatch++
			}
		}
		stats.timelineIndexes++
		return nil
	}); err != nil {
		return stats, err
	}
	if err := model.TimelinePosition.Iter(db, func(key, value []byte) error {
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid TimelinePosition key length %d", len(key))
		}
		viewer, _ := uuid.FromBytes(key[4 : 4+uuid.Size])
		entry, _ := uuid.FromBytes(key[4+uuid.Size:])
		if len(value) != 8 {
			return fmt.Errorf("invalid TimelinePosition value length %d", len(value))
		}
		ms := binary.BigEndian.Uint64(value)
		if ms > uint64(^uint64(0)>>1) {
			return errors.New("timeline position overflows int64")
		}
		activity := time.UnixMilli(int64(ms)).UTC()
		indexKey, err := model.TimelineIndexKey(viewer, entry, activity)
		if err != nil {
			return err
		}
		exists, err := db.Exists(indexKey)
		if err != nil {
			return err
		}
		_, mismatched := timelineMismatchedPairs[[2]uuid.UUID{viewer, entry}]
		if !exists && !mismatched {
			stats.timelineMissingIndex++
		}
		stats.timelinePositions++
		return nil
	}); err != nil {
		return stats, err
	}
	return stats, nil
}

func writeStoreAudit(out io.Writer, stats storeAuditStats) {
	fmt.Fprintf(out, "entries=%d noncanonical_entries=%d entry_key_id_mismatches=%d entry_indexes=%d noncanonical_indexes=%d missing_direct=%d orphan_indexes=%d\n",
		stats.entries, stats.noncanonicalEntries, stats.entryKeyIDMismatches, stats.entryIndexes,
		stats.noncanonicalIndexes, stats.missingDirectIndexes, stats.orphanIndexes)
	fmt.Fprintf(out, "timeline_indexes=%d timeline_positions=%d missing_entry=%d missing_position=%d missing_index=%d duplicates=%d timestamp_mismatch=%d\n",
		stats.timelineIndexes, stats.timelinePositions, stats.timelineMissingEntry, stats.timelineMissingPos,
		stats.timelineMissingIndex, stats.timelineDuplicates, stats.timelineTimeMismatch)
	fmt.Fprintf(out, "same_second_groups=%d same_second_entries=%d\n", stats.sameSecondGroups, stats.sameSecondEntries)
	fmt.Fprintf(out, "follow=%d follower=%d missing_follower=%d missing_follow=%d max_followers=%d\n",
		stats.followEdges, stats.followerEdges, stats.missingFollowerEdges, stats.missingFollowEdges, stats.maxFollowers)
}
