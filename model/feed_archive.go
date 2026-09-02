package model

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/protobuf/proto"
)

// FeedArchiveVersion is bumped whenever the snapshot contents change meaning;
// older snapshots fail the version check and are rebuilt on the next
// authenticated read or offline rebuild.
const FeedArchiveVersion int32 = 2

var feedArchiveMetaPrefix = []byte("feed-archive/v1/")
var feedArchiveDirtyMetaPrefix = []byte("feed-archive-dirty/v1/")

// FeedArchiveMetaKey identifies one rebuildable direct-Feed archive snapshot.
// The raw UUID suffix keeps the Meta key fixed-width and avoids textual UUID
// ambiguity.
func FeedArchiveMetaKey(feed uuid.UUID) store.Key {
	return NewKeyFrom(TableMeta.Bytes(), feedArchiveMetaPrefix, feed.Bytes())
}

// FeedArchiveDirtyMetaKey records when a direct Feed snapshot first became
// stale. Later mutations preserve that timestamp so an active Feed cannot
// postpone maintenance indefinitely.
func FeedArchiveDirtyMetaKey(feed uuid.UUID) store.Key {
	return NewKeyFrom(TableMeta.Bytes(), feedArchiveDirtyMetaPrefix, feed.Bytes())
}

func FeedArchiveDirtySince(db *store.Store, feed uuid.UUID) (time.Time, error) {
	if feed == uuid.Nil {
		return time.Time{}, errors.New("feed UUID is required")
	}
	raw, err := db.Get(FeedArchiveDirtyMetaKey(feed))
	if err != nil {
		return time.Time{}, err
	}
	if len(raw) != 8 {
		return time.Time{}, fmt.Errorf("Feed archive dirty marker for %s has invalid length %d", feed, len(raw))
	}
	ms := binary.BigEndian.Uint64(raw)
	if ms > uint64(^uint64(0)>>1) {
		return time.Time{}, fmt.Errorf("Feed archive dirty marker for %s has invalid timestamp", feed)
	}
	return time.UnixMilli(int64(ms)).UTC(), nil
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
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		if err := batch.Set(FeedArchiveMetaKey(feed), raw, nil); err != nil {
			return err
		}
		return batch.Delete(FeedArchiveDirtyMetaKey(feed), nil)
	})
}

// PurgeFeedArchives removes every rebuildable archive snapshot and dirty
// marker while leaving unrelated Meta records untouched.
func PurgeFeedArchives(db *store.Store) (snapshots, dirty int, err error) {
	prefixes := []struct {
		key   store.Key
		count *int
	}{
		{NewKeyFrom(TableMeta.Bytes(), feedArchiveMetaPrefix), &snapshots},
		{NewKeyFrom(TableMeta.Bytes(), feedArchiveDirtyMetaPrefix), &dirty},
	}
	for _, prefix := range prefixes {
		*prefix.count, err = db.ForwardScan(prefix.key, func(int, []byte, []byte) error { return nil })
		if err != nil {
			return 0, 0, err
		}
	}
	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		for _, prefix := range prefixes {
			upper := store.KeyUpperBound(prefix.key)
			if upper == nil {
				return fmt.Errorf("Feed archive prefix %x has no upper bound", prefix.key)
			}
			if err := batch.DeleteRange(prefix.key, upper, nil); err != nil {
				return err
			}
		}
		return nil
	})
	return snapshots, dirty, err
}

// StageMarkFeedArchiveDirty preserves the first mutation time and leaves the
// last good snapshot readable while deferred maintenance is pending. It must
// be called inside Store.ApplyBatch so its read and staged write are
// serialized with other archive mutations.
func StageMarkFeedArchiveDirty(db *store.Store, batch *pebble.Batch, feed uuid.UUID, at time.Time) error {
	if db == nil || batch == nil || feed == uuid.Nil || at.IsZero() {
		return errors.New("store, batch, feed UUID, and dirty time are required")
	}
	if _, err := db.Get(FeedArchiveDirtyMetaKey(feed)); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	ms := at.UTC().UnixMilli()
	if ms < 0 {
		return errors.New("Feed archive dirty time predates Unix epoch")
	}
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(ms))
	return batch.Set(FeedArchiveDirtyMetaKey(feed), raw[:], nil)
}

// BuildFeedArchive streams one direct EntryIndex from newest to oldest. The
// cursor for each year is recorded once, when iteration first enters the
// year: it is the position of the last row of the previous (newer) year, so
// the existing cursor contract skips that anchor and lands on the target
// year's newest Entry. The newest year has no newer boundary and gets no
// cursor; its link is simply the Feed's first page. This is independent of
// the caller's page size.
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
			y := &pb.FeedArchiveYear{Year: year}
			if len(previous) > 0 {
				// Anchor on the last row of the previous (newer) year; the
				// read path skips the anchor and starts at this year's
				// newest Entry.
				y.Cursor = util.Base58Encode(previous[len(prefix):])
			}
			stats.Years = append(stats.Years, y)
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
