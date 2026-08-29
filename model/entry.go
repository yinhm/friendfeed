package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func PutEntry(db *store.Store, entry *pb.Entry) (store.Key, error) {
	return PutEntryWithTimelineObserver(db, entry, nil)
}

// PutEntryWithTimelineObserver writes the canonical Entry and observes only
// the committed per-viewer Home moves caused by its derived fanout.
func PutEntryWithTimelineObserver(db *store.Store, entry *pb.Entry, observer TimelineMoveObserver) (store.Key, error) {
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
	groupUUID, groupEntry, err := entryGroupUUID(db, entry)
	if err != nil {
		return nil, err
	}
	created := false
	archiveDirtyAt := time.Now().UTC()
	if err := db.ApplyBatch(func(batch *pebble.Batch) error {
		if _, err := db.Get(key); errors.Is(err, store.ErrNotFound) {
			created = true
		} else if err != nil {
			return err
		}
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
		if created {
			if err := StageMarkFeedArchiveDirty(db, batch, userUuid, archiveDirtyAt); err != nil {
				return fmt.Errorf("mark author Feed archive dirty: %w", err)
			}
			if feedUuid != userUuid {
				if err := StageMarkFeedArchiveDirty(db, batch, feedUuid, archiveDirtyAt); err != nil {
					return fmt.Errorf("mark target Feed archive dirty: %w", err)
				}
			}
		}
		if err := writeEntryInteractionsBatch(batch, entryUuid, entry.Comments, entry.Likes); err != nil {
			return fmt.Errorf("write entry interactions: %w", err)
		}
		if created && groupEntry {
			if err := stageAdjustGroupActivityIfMember(db, batch, userUuid, groupUUID, GroupActivityPostScore); err != nil {
				return fmt.Errorf("update Group activity: %w", err)
			}
			if err := StageMoveGroupIndex(db, batch, groupUUID, time.Now().UTC()); err != nil {
				return fmt.Errorf("update Group discovery index: %w", err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Initialize activity-ranked Home timelines after the canonical Entry and
	// direct indexes commit. Follower count is unbounded.
	if _, err := FanoutTimelineActivity(db, entry, oldtime, TimelineActivityPublish, observer); err != nil {
		return nil, fmt.Errorf("fanout entry: %w", err)
	}

	// index entry body
	if entry.Body != "" {
		search.Indexer.Index(entry.Id, entry.Body)
	}

	return key, nil
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
	groupUUID, groupEntry, err := entryGroupUUID(db, entry)
	if err != nil {
		return err
	}
	// Activity timeline rows are derived, unbounded fanout state. Readers
	// remove stale rows lazily; audit/rebuild handles rows never read again.
	archiveDirtyAt := time.Now().UTC()
	if err := db.ApplyBatch(func(batch *pebble.Batch) error {
		if err := EntryIndex.removeIndexBatch(batch, profileUuid, oldtime, entryKey); err != nil {
			return fmt.Errorf("remove author entry index: %w", err)
		}
		if feedUuid != profileUuid {
			if err := EntryIndex.removeIndexBatch(batch, feedUuid, oldtime, entryKey); err != nil {
				return fmt.Errorf("remove feed entry index: %w", err)
			}
		}
		if err := StageMarkFeedArchiveDirty(db, batch, profileUuid, archiveDirtyAt); err != nil {
			return fmt.Errorf("mark author Feed archive dirty: %w", err)
		}
		if feedUuid != profileUuid {
			if err := StageMarkFeedArchiveDirty(db, batch, feedUuid, archiveDirtyAt); err != nil {
				return fmt.Errorf("mark target Feed archive dirty: %w", err)
			}
		}
		if err := batch.Delete(entryKey, nil); err != nil {
			return fmt.Errorf("delete entry: %w", err)
		}
		if err := deleteEntryInteractionsBatch(db, batch, entryUUID); err != nil {
			return fmt.Errorf("delete entry interactions: %w", err)
		}
		if groupEntry {
			deltas := map[uuid.UUID]int64{profileUuid: -GroupActivityPostScore}
			for _, like := range entry.Likes {
				if actor, parseErr := uuid.FromString(like.GetFrom().GetUuid()); parseErr == nil && actor != uuid.Nil {
					deltas[actor] -= GroupActivityLikeScore
				}
			}
			for _, comment := range entry.Comments {
				if actor, parseErr := uuid.FromString(comment.GetFrom().GetUuid()); parseErr == nil && actor != uuid.Nil {
					deltas[actor] -= GroupActivityCommentScore
				}
			}
			for actor, delta := range deltas {
				if err := stageAdjustGroupActivityIfMember(db, batch, actor, groupUUID, delta); err != nil {
					return fmt.Errorf("update Group activity for %s: %w", actor, err)
				}
			}
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

func PutTweet(db *store.Store, tweet *pb.Tweet) error {
	_, err := Tweet.Put(db, []byte(tweet.Id), tweet)
	return err
}
