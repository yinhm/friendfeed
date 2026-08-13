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

type timelineCandidateCursor struct {
	iter      *store.Iterator
	entry     uuid.UUID
	published time.Time
}

type timelineCandidateHeap []*timelineCandidateCursor

func (h timelineCandidateHeap) Len() int { return len(h) }
func (h timelineCandidateHeap) Less(i, j int) bool {
	if !h[i].published.Equal(h[j].published) {
		return h[i].published.After(h[j].published)
	}
	return bytes.Compare(h[i].entry.Bytes(), h[j].entry.Bytes()) < 0
}
func (h timelineCandidateHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *timelineCandidateHeap) Push(value any) {
	*h = append(*h, value.(*timelineCandidateCursor))
}
func (h *timelineCandidateHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

func timelineCandidateAt(iter *store.Iterator, retention time.Duration, now time.Time) (uuid.UUID, time.Time, bool, error) {
	if !iter.Valid() {
		return uuid.Nil, time.Time{}, false, iter.Error()
	}
	_, entry, published, err := ParseEntryIndexKey(iter.UnsafeKey())
	if err != nil {
		return uuid.Nil, time.Time{}, false, err
	}
	if retention != TimelineRetentionMax && published.Before(now.Add(-retention)) {
		return uuid.Nil, time.Time{}, false, nil
	}
	return entry, published, true, nil
}

// BuildHomeTimeline merges direct feeds newest-first, then recomputes activity
// for at most maxRows unique publish candidates. Long-tail entries outside the
// candidate set are deliberately not recovered without an activity index.
func BuildHomeTimeline(db *store.Store, feeds []uuid.UUID, maxRows int, retention time.Duration, now time.Time) (map[uuid.UUID]time.Time, int, error) {
	if maxRows <= 0 {
		return nil, 0, errors.New("timeline max rows must be positive")
	}
	if retention <= 0 {
		return nil, 0, errors.New("timeline retention must be positive")
	}
	now = now.UTC()
	rows := make(map[uuid.UUID]time.Time, maxRows)
	cursors := make([]*timelineCandidateCursor, 0, len(feeds))
	defer func() {
		for _, cursor := range cursors {
			_ = cursor.iter.Close()
		}
	}()

	queue := make(timelineCandidateHeap, 0, len(feeds))
	for _, feed := range feeds {
		if feed == uuid.Nil {
			return nil, 0, errors.New("timeline source feed UUID is zero")
		}
		iter, err := db.NewIterator(NewUUIDKey(TableEntryIndex, feed))
		if err != nil {
			return nil, 0, err
		}
		cursor := &timelineCandidateCursor{iter: iter}
		cursors = append(cursors, cursor)
		iter.First()
		entry, published, ok, err := timelineCandidateAt(iter, retention, now)
		if err != nil {
			return nil, 0, err
		}
		if ok {
			cursor.entry, cursor.published = entry, published
			heap.Push(&queue, cursor)
		}
	}

	for queue.Len() > 0 && len(rows) < maxRows {
		cursor := heap.Pop(&queue).(*timelineCandidateCursor)
		if _, exists := rows[cursor.entry]; !exists {
			rows[cursor.entry] = cursor.published
		}
		cursor.iter.Next()
		entry, published, ok, err := timelineCandidateAt(cursor.iter, retention, now)
		if err != nil {
			return nil, 0, err
		}
		if ok {
			cursor.entry, cursor.published = entry, published
			heap.Push(&queue, cursor)
		}
	}

	skipped, err := loadHomeTimelineActivities(db, rows, now)
	return rows, skipped, err
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

// ReplaceHomeTimeline clears and writes one viewer's two derived tables using
// bounded batches. TimelineState is intentionally updated by the caller only
// after this operation succeeds.
func ReplaceHomeTimeline(db *store.Store, viewer uuid.UUID, rows map[uuid.UUID]time.Time) error {
	indexPrefix := TimelineIndexPrefix(viewer)
	positionPrefix := NewKeyFrom(TimelinePosition.Prefix, viewer.Bytes())
	if err := db.ApplyBatch(func(batch *pebble.Batch) error {
		for _, prefix := range []store.Key{indexPrefix, positionPrefix} {
			upper := store.KeyUpperBound(prefix)
			if upper == nil {
				return fmt.Errorf("timeline prefix %x has no upper bound", prefix)
			}
			if err := batch.DeleteRange(prefix, upper, nil); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	type row struct {
		entry    uuid.UUID
		activity time.Time
	}
	batchRows := make([]row, 0, timelineWriteBatchSize)
	flush := func() error {
		if len(batchRows) == 0 {
			return nil
		}
		err := db.ApplyBatch(func(batch *pebble.Batch) error {
			for _, item := range batchRows {
				indexKey, err := TimelineIndexKey(viewer, item.entry, item.activity)
				if err != nil {
					return err
				}
				var position [8]byte
				binary.BigEndian.PutUint64(position[:], uint64(item.activity.UTC().UnixMilli()))
				if err := batch.Set(indexKey, nil, nil); err != nil {
					return err
				}
				if err := batch.Set(TimelinePositionKey(viewer, item.entry), position[:], nil); err != nil {
					return err
				}
			}
			return nil
		})
		batchRows = batchRows[:0]
		return err
	}
	for entry, activity := range rows {
		batchRows = append(batchRows, row{entry: entry, activity: activity})
		if len(batchRows) == timelineWriteBatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}
