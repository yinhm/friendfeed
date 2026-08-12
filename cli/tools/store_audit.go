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
	expectedDirectIndexes := 0
	foundDirectIndexes := 0

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
			expectedDirectIndexes++
			value, err := db.Get(expectedEntryIndexKey(db, owner, date, canonicalKey))
			if errors.Is(err, store.ErrNotFound) || (err == nil && !bytes.Equal(value, canonicalKey)) {
				stats.missingDirectIndexes++
				continue
			}
			if err != nil {
				return err
			}
			foundDirectIndexes++
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
		if followerCount > 0 && feed != followerFeed {
			finishFollowerFeed()
			followerCount = 0
		}
		followerFeed = feed
		followerCount++
		stats.followerEdges++
		return nil
	}); err != nil {
		return stats, err
	}
	finishFollowerFeed()
	// Follow keys are unique. The first pass already counted every Follow row
	// whose Follower counterpart exists, so the remaining Follower rows are
	// precisely the reverse-only edges; a second point lookup per row is waste.
	matchedGraphEdges := stats.followEdges - stats.missingFollowerEdges
	stats.missingFollowEdges = stats.followerEdges - matchedGraphEdges
	if stats.missingFollowEdges < 0 {
		return stats, errors.New("Follower table contains duplicate keys")
	}

	var groupOwner uuid.UUID
	var groupSecond int64
	groupCount := 0
	canonicalIndexes := 0
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
		if _, err := canonicalEntryKeyFromIndexValue(value); err != nil {
			stats.noncanonicalIndexes++
			stats.entryIndexes++
			return nil
		}
		canonicalIndexes++
		// EntryIndex is ordered by owner then reverse Flake. Its first eight
		// Flake bytes encode the deterministic reverse second; grouping here
		// detects collision-prone direct-index positions without reading Entry.
		const reverseTimestampOffset = 4 + uuid.Size
		if len(key) < reverseTimestampOffset+8 {
			return fmt.Errorf("invalid EntryIndex key length %d", len(key))
		}
		second := int64(binary.BigEndian.Uint64(key[reverseTimestampOffset : reverseTimestampOffset+8]))
		if groupCount > 0 && (owner != groupOwner || second != groupSecond) {
			finishSameSecond()
			groupCount = 0
		}
		groupOwner, groupSecond = owner, second
		groupCount++
		stats.entryIndexes++
		return nil
	}); err != nil {
		return stats, err
	}
	finishSameSecond()
	// Every healthy direct index was found by its exact deterministic key while
	// scanning Entry. Any additional canonical EntryIndex row is therefore an
	// orphan; this avoids reading Entry once for every index row.
	stats.orphanIndexes = canonicalIndexes - foundDirectIndexes
	if stats.orphanIndexes < 0 || foundDirectIndexes > expectedDirectIndexes {
		return stats, errors.New("direct index accounting is inconsistent")
	}

	// Keep only timestamp-mismatched pairs without their canonical index.
	// Healthy pairs and duplicate indexes are accounted for with counters.
	timelineMismatchedOnly := make(map[[2]uuid.UUID]struct{})
	matchedTimelinePositions := 0
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
				timelineMismatchedOnly[[2]uuid.UUID{viewer, entry}] = struct{}{}
			}
		} else {
			matchedTimelinePositions++
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
		if len(value) != 8 {
			return fmt.Errorf("invalid TimelinePosition value length %d", len(value))
		}
		ms := binary.BigEndian.Uint64(value)
		if ms > uint64(^uint64(0)>>1) {
			return errors.New("timeline position overflows int64")
		}
		stats.timelinePositions++
		return nil
	}); err != nil {
		return stats, err
	}
	// A Position is accounted for either by its exact index or by one known
	// timestamp-mismatched index. Everything left has no index at all.
	stats.timelineMissingIndex = stats.timelinePositions - matchedTimelinePositions - len(timelineMismatchedOnly)
	if stats.timelineMissingIndex < 0 {
		return stats, errors.New("timeline position accounting is inconsistent")
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
