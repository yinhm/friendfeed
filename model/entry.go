package model

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func PutEntry(db *store.Store, entry *pb.Entry) (store.Key, error) {
	if entry.FeedUuid == "" {
		entry.FeedUuid = entry.ProfileUuid // backward compatible
	}
	userUuid, err := uuid.FromString(entry.ProfileUuid)
	if err != nil {
		return nil, err
	}
	feedUuid, err := uuid.FromString(entry.FeedUuid)
	if err != nil {
		return nil, err
	}

	// unique key:
	// | table | entry uuid |

	entryUuid, err := uuid.FromString(entry.Id)
	if err != nil {
		return nil, err
	}
	oldtime, err := time.Parse(time.RFC3339, entry.Date)
	if err != nil {
		return nil, err
	}
	storedEntry := proto.Clone(entry).(*pb.Entry)
	storedEntry.Likes = nil
	storedEntry.Comments = nil
	encodedEntry, err := proto.Marshal(storedEntry)
	if err != nil {
		return nil, err
	}

	// Keep the entry record and its direct author/group indexes consistent.
	// Timeline fanout remains outside this batch because follower count is
	// unbounded and must not inflate one synchronous Pebble commit.
	key := Entry.PrefixAppend(entryUuid.Bytes())
	if err := db.ApplyBatch(func(batch *pebble.Batch) error {
		if err := batch.Set(key, encodedEntry, nil); err != nil {
			return fmt.Errorf("write entry: %w", err)
		}
		if err := EntryIndex.indexBatch(batch, userUuid, oldtime, key); err != nil {
			return fmt.Errorf("index entry for author: %w", err)
		}
		if userUuid != feedUuid {
			if err := EntryIndex.indexBatch(batch, feedUuid, oldtime, key); err != nil {
				return fmt.Errorf("index entry for feed: %w", err)
			}
		}
		if err := writeEntryInteractionsBatch(batch, entryUuid, entry.Comments, entry.Likes); err != nil {
			return fmt.Errorf("write entry interactions: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Initialize activity-ranked Home timelines after the canonical Entry and
	// direct indexes commit. Follower count is unbounded.
	if _, err := FanoutTimelineActivity(db, entry, oldtime, TimelineActivityPublish); err != nil {
		return nil, fmt.Errorf("fanout entry: %w", err)
	}

	// index entry body
	if entry.Body != "" {
		search.Indexer.Index(entry.Id, entry.Body)
	}

	return key, nil
}

// FanoutEntry maintains the retired EntryIndex timeline format.
// Deprecated: runtime Home timelines use FanoutTimelineActivity.
func FanoutEntry(db *store.Store, userUuid, feedUuid uuid.UUID,
	oldtime time.Time, entryKey store.Key) (n int, err error) {
	started := time.Now()
	defer func() {
		slog.Info("entry fanout", "feed_uuid", feedUuid.String(), "followers", n,
			"elapsed", time.Since(started), "error", err)
	}()
	fanOutToTimeline := TimelineUUID(userUuid)
	// fmt.Println(hex.EncodeToString(fanOutToTimeline.Bytes()))
	if err := EntryIndex.Index(db, fanOutToTimeline, oldtime, entryKey); err != nil {
		return 0, fmt.Errorf("index author timeline: %w", err)
	}

	n, err = updateFollowerTimelines(db, feedUuid, func(timelineUuid uuid.UUID) error {
		return EntryIndex.Index(db, timelineUuid, oldtime, entryKey)
	})
	if err != nil {
		return n, fmt.Errorf("index follower timeline: %w", err)
	}
	return n, nil
}

func GetEntry(db *store.Store, uuidStr string) (*pb.Entry, error) {
	entryUUID, err := uuid.FromString(uuidStr)
	if err != nil {
		return nil, err
	}

	entry := new(pb.Entry)
	err = Entry.Get(db, entryUUID.Bytes(), entry)
	if err != nil {
		return nil, fmt.Errorf("entry %s: %w", uuidStr, err)
	}
	if err := LoadEntryInteractions(db, entry); err != nil {
		return nil, fmt.Errorf("entry %s interactions: %w", uuidStr, err)
	}
	return entry, nil
}

func DeleteEntry(db *store.Store, uuidStr string) error {
	entryUUID, err := uuid.FromString(uuidStr)
	if err != nil {
		return err
	}

	entry, err := GetEntry(db, uuidStr)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil // blind delete
		}
		return err
	}

	// delete entry from user feed
	// index entry key
	oldtime, err := time.Parse(time.RFC3339, entry.Date)
	if err != nil {
		return err
	}
	profileUuid, err := uuid.FromString(entry.ProfileUuid)
	if err != nil {
		return err
	}
	feedUuid := profileUuid
	if entry.FeedUuid != "" {
		feedUuid, err = uuid.FromString(entry.FeedUuid)
		if err != nil {
			return err
		}
	}
	entryKey := Entry.PrefixAppend(entryUUID.Bytes())
	// Activity timeline rows are derived, unbounded fanout state. Readers
	// remove stale rows lazily; audit/rebuild handles rows never read again.
	if err := db.ApplyBatch(func(batch *pebble.Batch) error {
		if err := EntryIndex.removeIndexBatch(batch, profileUuid, oldtime, entryKey); err != nil {
			return fmt.Errorf("remove author entry index: %w", err)
		}
		if feedUuid != profileUuid {
			if err := EntryIndex.removeIndexBatch(batch, feedUuid, oldtime, entryKey); err != nil {
				return fmt.Errorf("remove feed entry index: %w", err)
			}
		}
		if err := batch.Delete(entryKey, nil); err != nil {
			return fmt.Errorf("delete entry: %w", err)
		}
		if err := deleteEntryInteractionsBatch(db, batch, entryUUID); err != nil {
			return fmt.Errorf("delete entry interactions: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	// Keep the search index in sync; a leftover document goes stale and
	// Search then has to drop it lazily on a later query.
	if search.Indexer != nil {
		if err := search.Indexer.Delete(uuidStr); err != nil {
			return fmt.Errorf("delete entry from search index: %w", err)
		}
	}

	// delete entry from public index??
	return nil
}

// DeleteFanoutEntry removes rows from the retired EntryIndex timeline format.
// Deprecated: activity timeline rows are deleted lazily or by rebuild.
func DeleteFanoutEntry(db *store.Store, userUuid, feedUuid uuid.UUID,
	oldtime time.Time, entryKey store.Key) (n int, err error) {
	fanOutToTimeline := TimelineUUID(userUuid)
	if err := EntryIndex.RemoveIndex(db, fanOutToTimeline, oldtime, entryKey); err != nil {
		return 0, fmt.Errorf("remove author timeline index: %w", err)
	}

	n, err = updateFollowerTimelines(db, feedUuid, func(timelineUuid uuid.UUID) error {
		return EntryIndex.RemoveIndex(db, timelineUuid, oldtime, entryKey)
	})
	if err != nil {
		return n, fmt.Errorf("remove follower timeline index: %w", err)
	}
	return n, nil
}

func updateFollowerTimelines(db *store.Store, feedUuid uuid.UUID, update func(uuid.UUID) error) (n int, err error) {
	prefix := NewPrefixKeyFrom(TableFollower, feedUuid.Bytes())
	// fmt.Printf("scan key, %s\n", prefix.String())
	return db.ForwardScan(prefix, func(i int, k, v []byte) error {
		fk := ParseFollowerKey(k)
		timelineUuid := UniqueKeyFrom(fk.String(), "user", "timeline")
		// fmt.Printf("fanout to: <%s, %x>", fk.String(), timelineUuid)
		return update(timelineUuid)
	})
}

func PutTweet(db *store.Store, tweet *pb.Tweet) error {
	_, err := Tweet.Put(db, []byte(tweet.Id), tweet)
	return err
}
