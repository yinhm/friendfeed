package model

import (
	"bytes"
	"container/heap"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type timelineCandidate struct {
	entry     uuid.UUID
	published time.Time
}

type timelineCandidateHeap []*timelineCandidate

func (h timelineCandidateHeap) Len() int { return len(h) }
func (h timelineCandidateHeap) Less(i, j int) bool {
	if !h[i].published.Equal(h[j].published) {
		return h[i].published.Before(h[j].published)
	}
	return bytes.Compare(h[i].entry.Bytes(), h[j].entry.Bytes()) > 0
}
func (h timelineCandidateHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *timelineCandidateHeap) Push(value any) {
	*h = append(*h, value.(*timelineCandidate))
}
func (h *timelineCandidateHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

// SelectTimelineCandidates scans each reverse-time direct feed forward and
// keeps a maxRows min-heap of the globally newest unique publications. Only
// one Pebble iterator is open at a time, even for viewers following thousands
// of feeds. Long-tail entries outside the candidate set are deliberately not
// recovered without an activity index.
func SelectTimelineCandidates(db *store.Store, feeds []uuid.UUID, maxRows int, retention time.Duration, now time.Time) (map[uuid.UUID]time.Time, error) {
	if maxRows <= 0 {
		return nil, errors.New("timeline max rows must be positive")
	}
	if retention <= 0 {
		return nil, errors.New("timeline retention must be positive")
	}
	now = now.UTC()
	selected := make(map[uuid.UUID]*timelineCandidate, maxRows)
	queue := make(timelineCandidateHeap, 0, maxRows)
	cutoff := now.Add(-retention)
	for _, feed := range feeds {
		if feed == uuid.Nil {
			return nil, errors.New("timeline source feed UUID is zero")
		}
		iter, err := db.NewIterator(NewUUIDKey(TableEntryIndex, feed))
		if err != nil {
			return nil, err
		}
		for iter.First(); iter.Valid(); iter.Next() {
			_, entry, published, err := ParseEntryIndexKey(iter.UnsafeKey())
			if err != nil {
				_ = iter.Close()
				return nil, err
			}
			if retention != TimelineRetentionMax && published.Before(cutoff) {
				break
			}
			if _, exists := selected[entry]; exists {
				continue
			}
			candidate := &timelineCandidate{entry: entry, published: published}
			if queue.Len() < maxRows {
				selected[entry] = candidate
				heap.Push(&queue, candidate)
				continue
			}
			worst := queue[0]
			better := published.After(worst.published) ||
				(published.Equal(worst.published) && bytes.Compare(entry.Bytes(), worst.entry.Bytes()) < 0)
			if !better {
				break
			}
			removed := heap.Pop(&queue).(*timelineCandidate)
			delete(selected, removed.entry)
			selected[entry] = candidate
			heap.Push(&queue, candidate)
		}
		iterErr := iter.Error()
		closeErr := iter.Close()
		if err := errors.Join(iterErr, closeErr); err != nil {
			return nil, err
		}
	}

	rows := make(map[uuid.UUID]time.Time, len(selected))
	for entry, candidate := range selected {
		rows[entry] = candidate.published
	}
	return rows, nil
}

// BuildHomeTimeline selects the newest publications across the viewer's
// source feeds and recomputes each candidate's final activity from its
// current Like/Comment rows. Long-tail entries outside the candidate set are
// deliberately not recovered without an activity index.
func BuildHomeTimeline(db *store.Store, feeds []uuid.UUID, maxRows int, retention time.Duration, now time.Time) (map[uuid.UUID]time.Time, int, error) {
	rows, err := SelectTimelineCandidates(db, feeds, maxRows, retention, now)
	if err != nil {
		return nil, 0, err
	}
	skipped, err := loadHomeTimelineActivities(db, rows, now)
	return rows, skipped, err
}

// MergeHomeFeed adds at most maxRows recent entries from one newly-followed
// feed to viewer's existing Home cache. It never rebuilds unrelated feeds.
func MergeHomeFeed(db *store.Store, viewer, feed uuid.UUID, maxRows int, retention time.Duration, now time.Time) (added, skipped int, err error) {
	if viewer == uuid.Nil || feed == uuid.Nil {
		return 0, 0, errors.New("timeline viewer and feed UUID are required")
	}
	rows, skipped, err := BuildHomeTimeline(db, []uuid.UUID{feed}, maxRows, retention, now)
	if err != nil {
		return 0, skipped, err
	}
	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		for entry, activity := range rows {
			moved, err := stageMoveTimelineEntry(db, batch, viewer, entry, activity, nil)
			if err != nil {
				return err
			}
			if moved {
				added++
			}
		}
		return nil
	})
	if err != nil {
		return 0, skipped, err
	}
	if _, err := TrimHomeTimeline(db, viewer, TimelineMaxEntries, retention, now); err != nil {
		return added, skipped, err
	}
	return added, skipped, nil
}

// RemoveHomeFeed deletes only rows in viewer's bounded Home cache whose
// canonical Entry belongs to feed. Missing Entries are stale cache rows and
// are removed at the same time. Memory stays bounded by timelineWriteBatchSize.
func RemoveHomeFeed(db *store.Store, viewer, feed uuid.UUID) (int, error) {
	if viewer == uuid.Nil || feed == uuid.Nil {
		return 0, errors.New("timeline viewer and feed UUID are required")
	}
	type row struct {
		entry    uuid.UUID
		activity time.Time
	}
	deletes := make([]row, 0, timelineWriteBatchSize)
	removed := 0
	flush := func() error {
		if len(deletes) == 0 {
			return nil
		}
		if err := db.ApplyBatch(func(batch *pebble.Batch) error {
			for _, item := range deletes {
				if err := DeleteTimelinePositionBatch(batch, viewer, item.entry, item.activity); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		deletes = deletes[:0]
		return nil
	}
	_, err := db.ForwardScan(TimelineIndexPrefix(viewer), func(_ int, key, _ []byte) error {
		_, entryID, activity, err := ParseTimelineIndexKey(key)
		if err != nil {
			return err
		}
		entry := new(pb.Entry)
		err = Entry.Get(db, entryID.Bytes(), entry)
		remove := errors.Is(err, ErrNotFound)
		if err != nil && !remove {
			return err
		}
		if !remove {
			entryFeed := entry.FeedUuid
			if entryFeed == "" {
				entryFeed = entry.ProfileUuid
			}
			entryFeedUUID, parseErr := uuid.FromString(entryFeed)
			if parseErr != nil {
				return fmt.Errorf("entry %s feed UUID: %w", entry.Id, parseErr)
			}
			remove = entryFeedUUID == feed
		}
		if !remove {
			return nil
		}
		deletes = append(deletes, row{entry: entryID, activity: activity})
		removed++
		if len(deletes) == timelineWriteBatchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return removed, err
	}
	return removed, flush()
}

func loadHomeTimelineActivities(db *store.Store, rows map[uuid.UUID]time.Time, now time.Time) (int, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i].Bytes(), ids[j].Bytes()) < 0 })

	entryIter, err := db.NewIterator(Entry.Prefix)
	if err != nil {
		return 0, err
	}
	defer entryIter.Close()
	likeIter, err := db.NewIterator(Like.Prefix)
	if err != nil {
		return 0, err
	}
	defer likeIter.Close()
	commentIter, err := db.NewIterator(Comment.Prefix)
	if err != nil {
		return 0, err
	}
	defer commentIter.Close()

	skipped := 0
	for _, id := range ids {
		entryKey := Entry.PrefixAppend(id.Bytes())
		entryIter.SeekGE(entryKey)
		if !entryIter.Valid() || !bytes.Equal(entryIter.UnsafeKey(), entryKey) {
			delete(rows, id)
			continue
		}
		entry := new(pb.Entry)
		if err := proto.Unmarshal(entryIter.UnsafeValue(), entry); err != nil {
			return skipped, fmt.Errorf("decode entry %s: %w", id, err)
		}
		likePrefix := NewKeyFrom(Like.Prefix, id.Bytes())
		likeIter.SeekGE(likePrefix)
		for likeIter.Valid() && bytes.HasPrefix(likeIter.UnsafeKey(), likePrefix) {
			like := new(pb.Like)
			if err := proto.Unmarshal(likeIter.UnsafeValue(), like); err != nil {
				return skipped, fmt.Errorf("decode like for entry %s: %w", id, err)
			}
			entry.Likes = append(entry.Likes, like)
			likeIter.Next()
		}
		commentPrefix := NewKeyFrom(Comment.Prefix, id.Bytes())
		commentIter.SeekGE(commentPrefix)
		for commentIter.Valid() && bytes.HasPrefix(commentIter.UnsafeKey(), commentPrefix) {
			comment := new(pb.Comment)
			if err := proto.Unmarshal(commentIter.UnsafeValue(), comment); err != nil {
				return skipped, fmt.Errorf("decode comment for entry %s: %w", id, err)
			}
			entry.Comments = append(entry.Comments, comment)
			commentIter.Next()
		}
		activity, ignored, err := rebuildHomeTimelineActivity(entry, now)
		skipped += ignored
		if err != nil {
			return skipped, fmt.Errorf("entry %s activity: %w", entry.Id, err)
		}
		rows[id] = activity
	}
	return skipped, errors.Join(entryIter.Error(), likeIter.Error(), commentIter.Error())
}

type homeTimelineEvent struct {
	at   time.Time
	kind TimelineActivityKind
	id   string
}

func rebuildHomeTimelineActivity(entry *pb.Entry, now time.Time) (time.Time, int, error) {
	published, err := time.Parse(time.RFC3339, entry.Date)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid publish date %q: %w", entry.Date, err)
	}
	if published.After(now) {
		published = now
	}
	events := make([]homeTimelineEvent, 0, len(entry.Likes)+len(entry.Comments))
	skipped := 0
	for _, like := range entry.Likes {
		if like == nil {
			skipped++
			continue
		}
		at, err := time.Parse(time.RFC3339, like.Date)
		if err != nil {
			skipped++
			continue
		}
		id := ""
		if like.From != nil {
			id = like.From.Uuid
		}
		events = append(events, homeTimelineEvent{at: at, kind: TimelineActivityLike, id: id})
	}
	for _, comment := range entry.Comments {
		if comment == nil {
			skipped++
			continue
		}
		at, err := time.Parse(time.RFC3339, comment.Date)
		if err != nil {
			skipped++
			continue
		}
		events = append(events, homeTimelineEvent{at: at, kind: TimelineActivityComment, id: comment.Id})
	}
	sort.Slice(events, func(i, j int) bool {
		if !events[i].at.Equal(events[j].at) {
			return events[i].at.Before(events[j].at)
		}
		if events[i].kind != events[j].kind {
			return events[i].kind < events[j].kind
		}
		return events[i].id < events[j].id
	})
	activity := published
	for _, event := range events {
		if event.at.After(now) || !event.at.After(activity) {
			continue
		}
		if event.kind == TimelineActivityLike {
			age := event.at.Sub(published)
			if age < 0 || age > LikeBumpMaxEntryAge || event.at.Sub(activity) < LikeBumpCooldown {
				continue
			}
		}
		activity = event.at
	}
	return activity, skipped, nil
}

const timelineWriteBatchSize = 500

// ReplaceHomeTimeline atomically replaces one viewer's two derived tables.
// Callers bound rows to TimelineMaxEntries, so the single batch is bounded and
// an error cannot expose a partially replaced stale cache. TimelineState is
// intentionally updated by the caller only after this operation succeeds.
func ReplaceHomeTimeline(db *store.Store, viewer uuid.UUID, rows map[uuid.UUID]time.Time) error {
	indexPrefix := TimelineIndexPrefix(viewer)
	positionPrefix := NewKeyFrom(TimelinePosition.Prefix, viewer.Bytes())
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		for _, prefix := range []store.Key{indexPrefix, positionPrefix} {
			upper := store.KeyUpperBound(prefix)
			if upper == nil {
				return fmt.Errorf("timeline prefix %x has no upper bound", prefix)
			}
			if err := batch.DeleteRange(prefix, upper, nil); err != nil {
				return err
			}
		}
		for entry, activity := range rows {
			indexKey, err := TimelineIndexKey(viewer, entry, activity)
			if err != nil {
				return err
			}
			var position [8]byte
			binary.BigEndian.PutUint64(position[:], uint64(activity.UTC().UnixMilli()))
			if err := batch.Set(indexKey, nil, nil); err != nil {
				return err
			}
			if err := batch.Set(TimelinePositionKey(viewer, entry), position[:], nil); err != nil {
				return err
			}
		}
		return nil
	})
}

// TrimHomeTimeline keeps the newest maxRows existing activity rows within the
// optional activity-time retention window. It deletes Index and Position in
// fixed batches and does not inspect canonical Entry or interactions.
func TrimHomeTimeline(db *store.Store, viewer uuid.UUID, maxRows int, retention time.Duration, now time.Time) (int, error) {
	if maxRows <= 0 || retention <= 0 {
		return 0, errors.New("invalid timeline trim bounds")
	}
	type row struct {
		entry    uuid.UUID
		activity time.Time
	}
	deletes := make([]row, 0, timelineWriteBatchSize)
	deleted := 0
	flush := func() error {
		if len(deletes) == 0 {
			return nil
		}
		err := db.ApplyBatch(func(batch *pebble.Batch) error {
			for _, item := range deletes {
				if err := DeleteTimelinePositionBatch(batch, viewer, item.entry, item.activity); err != nil {
					return err
				}
			}
			return nil
		})
		deletes = deletes[:0]
		return err
	}
	rows := 0
	cutoff := now.UTC().Add(-retention)
	_, err := db.ForwardScan(TimelineIndexPrefix(viewer), func(_ int, key, _ []byte) error {
		_, entry, activity, err := ParseTimelineIndexKey(key)
		if err != nil {
			return err
		}
		rows++
		outsideTime := retention != TimelineRetentionMax && activity.Before(cutoff)
		if rows > maxRows || outsideTime {
			deletes = append(deletes, row{entry: entry, activity: activity})
			deleted++
			if len(deletes) == timelineWriteBatchSize {
				return flush()
			}
		}
		return nil
	})
	if err != nil {
		return deleted, err
	}
	return deleted, flush()
}
