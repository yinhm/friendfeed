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
)

const (
	LikeBumpMaxEntryAge = 7 * 24 * time.Hour
	LikeBumpCooldown    = 10 * time.Minute
	TimelineActiveFor   = 30 * 24 * time.Hour
	TimelineTouchAfter  = time.Hour
	TimelineMaxEntries  = 10_000
	TimelineColdEntries = 500
	// TimelineRetentionMax disables the publish-time cutoff while retaining a
	// concrete option that can later be changed to 90 days or another window.
	TimelineRetentionMax = time.Duration(1<<63 - 1)
)

func TimelineStateKey(viewer uuid.UUID) store.Key {
	return NewKeyFrom(TimelineState.Prefix, viewer.Bytes())
}

func validateTimelineViewer(viewer uuid.UUID) error {
	if viewer == uuid.Nil {
		return errors.New("timeline viewer UUID is zero")
	}
	return nil
}

func TimelineLastAccess(db *store.Store, viewer uuid.UUID) (time.Time, error) {
	if err := validateTimelineViewer(viewer); err != nil {
		return time.Time{}, err
	}
	raw, err := db.Get(TimelineStateKey(viewer))
	if err != nil {
		return time.Time{}, err
	}
	return ParseTimelineLastAccess(raw)
}

// ParseTimelineLastAccess decodes a TimelineState value already obtained by a
// table scan. It avoids a second point lookup when migration tools enumerate
// TimelineState.
func ParseTimelineLastAccess(raw []byte) (time.Time, error) {
	if len(raw) != 8 {
		return time.Time{}, fmt.Errorf("invalid TimelineState value length %d", len(raw))
	}
	ms := binary.BigEndian.Uint64(raw)
	if ms > uint64(^uint64(0)>>1) {
		return time.Time{}, errors.New("timeline last access overflows int64")
	}
	return time.UnixMilli(int64(ms)).UTC(), nil
}

func TouchTimelineState(db *store.Store, viewer uuid.UUID, at time.Time) error {
	if err := validateTimelineViewer(viewer); err != nil {
		return err
	}
	ms := at.UTC().UnixMilli()
	if ms < 0 {
		return fmt.Errorf("timeline last access before Unix epoch: %s", at)
	}
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], uint64(ms))
	return db.Set(TimelineStateKey(viewer), value[:])
}

func TimelineIsActive(db *store.Store, viewer uuid.UUID, now time.Time) (bool, error) {
	last, err := TimelineLastAccess(db, viewer)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	age := now.UTC().Sub(last)
	return age >= 0 && age <= TimelineActiveFor, nil
}

func DeleteTimelineState(db *store.Store, viewer uuid.UUID) error {
	if err := validateTimelineViewer(viewer); err != nil {
		return err
	}
	return db.Delete(TimelineStateKey(viewer))
}

func TimelineIndexPrefix(viewer uuid.UUID) store.Key {
	return NewKeyFrom(TimelineIndex.Prefix, viewer.Bytes())
}

func TimelinePositionKey(viewer, entry uuid.UUID) store.Key {
	return NewKeyFrom(TimelinePosition.Prefix, viewer.Bytes(), entry.Bytes())
}

func reverseTimelineMillis(t time.Time) ([8]byte, error) {
	var encoded [8]byte
	ms := t.UTC().UnixMilli()
	if ms < 0 {
		return encoded, fmt.Errorf("timeline time before Unix epoch: %s", t)
	}
	binary.BigEndian.PutUint64(encoded[:], ^uint64(ms))
	return encoded, nil
}

func TimelineIndexKey(viewer, entry uuid.UUID, activity time.Time) (store.Key, error) {
	reverse, err := reverseTimelineMillis(activity)
	if err != nil {
		return nil, err
	}
	return NewKeyFrom(TimelineIndex.Prefix, viewer.Bytes(), reverse[:], entry.Bytes()), nil
}

func ParseTimelineIndexKey(key store.Key) (viewer, entry uuid.UUID, activity time.Time, err error) {
	const size = 4 + uuid.Size + 8 + uuid.Size
	if len(key) != size {
		return viewer, entry, activity, fmt.Errorf("invalid TimelineIndex key length %d", len(key))
	}
	viewer, err = uuid.FromBytes(key[4 : 4+uuid.Size])
	if err != nil {
		return viewer, entry, activity, err
	}
	ms := ^binary.BigEndian.Uint64(key[4+uuid.Size : 4+uuid.Size+8])
	if ms > uint64(^uint64(0)>>1) {
		return viewer, entry, activity, errors.New("timeline timestamp overflows int64")
	}
	entry, err = uuid.FromBytes(key[4+uuid.Size+8:])
	return viewer, entry, time.UnixMilli(int64(ms)).UTC(), err
}

func TimelinePositionTime(db *store.Store, viewer, entry uuid.UUID) (time.Time, error) {
	raw, err := db.Get(TimelinePositionKey(viewer, entry))
	if err != nil {
		return time.Time{}, err
	}
	if len(raw) != 8 {
		return time.Time{}, fmt.Errorf("invalid TimelinePosition value length %d", len(raw))
	}
	ms := binary.BigEndian.Uint64(raw)
	if ms > uint64(^uint64(0)>>1) {
		return time.Time{}, errors.New("timeline position overflows int64")
	}
	return time.UnixMilli(int64(ms)).UTC(), nil
}

// MoveTimelineEntry atomically replaces one viewer's old position. qualify is
// evaluated while ApplyBatch holds its serialization lock. Activity never
// moves backwards.
func MoveTimelineEntry(db *store.Store, viewer, entry uuid.UUID, activity time.Time,
	qualify func(old time.Time, exists bool) bool) (bool, error) {
	moved := false
	err := db.ApplyBatch(func(batch *pebble.Batch) error {
		var err error
		moved, err = stageMoveTimelineEntry(db, batch, viewer, entry, activity, qualify)
		return err
	})
	return moved, err
}

// stageMoveTimelineEntry is MoveTimelineEntry's batch form. Callers may stage
// multiple distinct viewer/entry pairs while one ApplyBatch lock is held.
func stageMoveTimelineEntry(db *store.Store, batch *pebble.Batch, viewer, entry uuid.UUID, activity time.Time,
	qualify func(old time.Time, exists bool) bool) (bool, error) {
	activity = activity.UTC()
	old, err := TimelinePositionTime(db, viewer, entry)
	exists := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	if exists && !activity.After(old) {
		return false, nil
	}
	if qualify != nil && !qualify(old, exists) {
		return false, nil
	}
	if exists {
		oldKey, err := TimelineIndexKey(viewer, entry, old)
		if err != nil {
			return false, err
		}
		if err := batch.Delete(oldKey, nil); err != nil {
			return false, err
		}
	}
	newKey, err := TimelineIndexKey(viewer, entry, activity)
	if err != nil {
		return false, err
	}
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], uint64(activity.UnixMilli()))
	if err := batch.Set(newKey, nil, nil); err != nil {
		return false, err
	}
	if err := batch.Set(TimelinePositionKey(viewer, entry), value[:], nil); err != nil {
		return false, err
	}
	return true, nil
}

func DeleteTimelinePositionBatch(batch *pebble.Batch, viewer, entry uuid.UUID, activity time.Time) error {
	key, err := TimelineIndexKey(viewer, entry, activity)
	if err != nil {
		return err
	}
	if err := batch.Delete(key, nil); err != nil {
		return err
	}
	return batch.Delete(TimelinePositionKey(viewer, entry), nil)
}

type TimelineActivityKind uint8

const (
	TimelineActivityPublish TimelineActivityKind = iota
	TimelineActivityLike
	TimelineActivityComment
)

// TimelineMoveObserver observes an already committed per-viewer timeline move.
// Observers are best-effort only: they must not block, persist data, or affect
// the correctness/error result of the timeline fanout.
type TimelineMoveObserver func(viewer, entry uuid.UUID, kind TimelineActivityKind, at time.Time)

const timelineFanoutBatchSize = 100

// FanoutTimelineActivity updates only active Home caches. Inactive viewers are
// rebuilt on their next Home request; source mutations are committed before
// this derived-data fanout. Moves commit atomically in batches of 100 viewers;
// a later batch may fail after earlier batches committed. observer runs only
// after a real committed move.
func FanoutTimelineActivity(db *store.Store, entry *pb.Entry, activity time.Time, kind TimelineActivityKind, observer TimelineMoveObserver) (int, error) {
	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return 0, err
	}
	author, err := uuid.FromString(entry.ProfileUuid)
	if err != nil {
		return 0, err
	}
	feed := author
	if entry.FeedUuid != "" {
		feed, err = uuid.FromString(entry.FeedUuid)
		if err != nil {
			return 0, err
		}
	}
	now := time.Now().UTC()
	if kind == TimelineActivityPublish && activity.After(now) {
		activity = now
	}
	if kind == TimelineActivityLike {
		published, err := time.Parse(time.RFC3339, entry.Date)
		if err != nil {
			return 0, err
		}
		age := activity.Sub(published)
		if age < 0 || age > LikeBumpMaxEntryAge {
			return 0, nil
		}
	}
	qualify := func(old time.Time, exists bool) bool { return true }
	if kind == TimelineActivityLike {
		qualify = func(old time.Time, exists bool) bool {
			return !exists || activity.Sub(old) >= LikeBumpCooldown
		}
	}
	viewers := make([]uuid.UUID, 0, timelineFanoutBatchSize)
	updated := 0
	flush := func() error {
		if len(viewers) == 0 {
			return nil
		}
		moved := make([]uuid.UUID, 0, len(viewers))
		err := db.ApplyBatch(func(batch *pebble.Batch) error {
			for _, viewer := range viewers {
				ok, err := stageMoveTimelineEntry(db, batch, viewer, entryUUID, activity, qualify)
				if err != nil {
					return err
				}
				if ok {
					moved = append(moved, viewer)
				}
			}
			return nil
		})
		viewers = viewers[:0]
		if err != nil {
			return err
		}
		updated += len(moved)
		if observer != nil {
			for _, viewer := range moved {
				observer(viewer, entryUUID, kind, activity)
			}
		}
		return nil
	}
	queue := func(viewer uuid.UUID) error {
		active, err := TimelineIsActive(db, viewer, now)
		if err != nil {
			return err
		}
		if !active {
			return nil
		}
		viewers = append(viewers, viewer)
		if len(viewers) == timelineFanoutBatchSize {
			return flush()
		}
		return nil
	}
	if err := queue(author); err != nil {
		return 0, fmt.Errorf("update author timeline: %w", err)
	}
	prefix := NewPrefixKeyFrom(TableFollower, feed.Bytes())
	_, err = db.ForwardScan(prefix, func(_ int, key, _ []byte) error {
		follower, err := uuid.FromBytes(ParseFollowerKey(key))
		if err != nil {
			return err
		}
		if follower == author {
			return nil
		}
		return queue(follower)
	})
	if err != nil {
		return updated, fmt.Errorf("update follower timelines: %w", err)
	}
	if err := flush(); err != nil {
		return updated, fmt.Errorf("update follower timelines: %w", err)
	}
	return updated, nil
}
