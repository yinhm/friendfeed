package model

import (
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/protobuf/proto"
)

const FeedArchiveVersion int32 = 1

var feedArchiveMetaPrefix = []byte("feed-archive/v1/")

// FeedArchiveMetaKey identifies one rebuildable direct-Feed archive snapshot.
// The raw UUID suffix keeps the Meta key fixed-width and avoids textual UUID
// ambiguity.
func FeedArchiveMetaKey(feed uuid.UUID) store.Key {
	return NewKeyFrom(TableMeta.Bytes(), feedArchiveMetaPrefix, feed.Bytes())
}

func GetFeedArchive(db *store.Store, feed uuid.UUID) (*pb.FeedArchiveStats, error) {
	if feed == uuid.Nil {
		return nil, errors.New("feed UUID is required")
	}
	raw, err := db.Get(FeedArchiveMetaKey(feed))
	if err != nil {
		return nil, err
	}
	stats := new(pb.FeedArchiveStats)
	if err := proto.Unmarshal(raw, stats); err != nil {
		return nil, fmt.Errorf("decode Feed archive for %s: %w", feed, err)
	}
	if stats.Version != FeedArchiveVersion {
		return nil, fmt.Errorf("Feed archive for %s has unsupported version %d", feed, stats.Version)
	}
	return stats, nil
}

func PutFeedArchive(db *store.Store, feed uuid.UUID, stats *pb.FeedArchiveStats) error {
	if feed == uuid.Nil || stats == nil {
		return errors.New("feed UUID and archive stats are required")
	}
	stats.Version = FeedArchiveVersion
	raw, err := proto.Marshal(stats)
	if err != nil {
		return err
	}
	return db.Set(FeedArchiveMetaKey(feed), raw)
}

// StageInvalidateFeedArchive removes a derived snapshot in the same mutation
// batch that changes its direct EntryIndex. A later authenticated read stages
// an idempotent rebuild task.
func StageInvalidateFeedArchive(batch *pebble.Batch, feed uuid.UUID) error {
	if batch == nil || feed == uuid.Nil {
		return errors.New("batch and feed UUID are required")
	}
	return batch.Delete(FeedArchiveMetaKey(feed), nil)
}

// BuildFeedArchive streams one direct EntryIndex from newest to oldest. The
// cursor for each year is the row immediately before that year's oldest row,
// so the existing cursor contract skips the anchor and lands on the year's
// last Entry. This is independent of the caller's page size.
func BuildFeedArchive(db *store.Store, feed uuid.UUID) (*pb.FeedArchiveStats, error) {
	if feed == uuid.Nil {
		return nil, errors.New("feed UUID is required")
	}
	prefix := NewUUIDKey(TableEntryIndex, feed)
	iter, err := db.NewIterator(prefix)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	stats := &pb.FeedArchiveStats{Version: FeedArchiveVersion}
	var previous store.Key
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		_, entry, published, err := ParseEntryIndexKey(key)
		if err != nil {
			return nil, err
		}
		if _, err := db.Get(Entry.PrefixAppend(entry.Bytes())); errors.Is(err, store.ErrNotFound) {
			continue
		} else if err != nil {
			return nil, err
		}

		year := int32(published.Year())
		if len(stats.Years) == 0 || stats.Years[len(stats.Years)-1].Year != year {
			stats.Years = append(stats.Years, &pb.FeedArchiveYear{Year: year})
		}
		if len(previous) > 0 {
			stats.Years[len(stats.Years)-1].Cursor = util.Base58Encode(previous[len(prefix):])
		}
		stats.EntryCount++
		stats.Years[len(stats.Years)-1].EntryCount++
		previous = key
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	return stats, nil
}
