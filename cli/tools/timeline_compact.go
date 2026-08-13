package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/store"
)

type timelineCompactOptions struct {
	user      string
	dryRun    bool
	maxRows   int
	retention time.Duration
	now       time.Time
}

type timelineCompactStats struct {
	viewers          int
	inactiveViewers  int
	indexes          int
	positions        int
	deletedIndexes   int
	deletedPositions int
}

func compactTimelineRanges(db *store.Store, viewer uuid.UUID, dryRun bool) error {
	if dryRun {
		return nil
	}
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		for _, prefix := range []store.Key{
			model.TimelineIndexPrefix(viewer),
			model.NewKeyFrom(model.TimelinePosition.Prefix, viewer.Bytes()),
		} {
			upper := store.KeyUpperBound(prefix)
			if upper == nil {
				return fmt.Errorf("timeline prefix %x has no upper bound", prefix)
			}
			if err := batch.DeleteRange(prefix, upper, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

func compactTimelines(db *store.Store, options timelineCompactOptions) (timelineCompactStats, error) {
	stats := timelineCompactStats{}
	if options.maxRows <= 0 {
		options.maxRows = model.TimelineMaxEntries
	}
	if options.retention <= 0 {
		options.retention = model.TimelineRetentionMax
	}
	if options.now.IsZero() {
		options.now = time.Now().UTC()
	}
	var selected uuid.UUID
	if options.user != "" {
		profile, err := model.GetProfileFromUserId(db, options.user)
		if err != nil {
			return stats, fmt.Errorf("resolve profile %q: %w", options.user, err)
		}
		selected, err = uuid.FromString(profile.Uuid)
		if err != nil {
			return stats, err
		}
	}

	const batchSize = 500
	type deleteRow struct {
		viewer   uuid.UUID
		entry    uuid.UUID
		activity time.Time
	}
	deletes := make([]deleteRow, 0, batchSize)
	flush := func() error {
		if options.dryRun || len(deletes) == 0 {
			deletes = deletes[:0]
			return nil
		}
		err := db.ApplyBatch(func(batch *pebble.Batch) error {
			for _, row := range deletes {
				if err := model.DeleteTimelinePositionBatch(batch, row.viewer, row.entry, row.activity); err != nil {
					return err
				}
			}
			return nil
		})
		deletes = deletes[:0]
		return err
	}

	var current uuid.UUID
	currentRows := 0
	currentActive := false
	seenViewer := false
	finishViewer := func() error {
		if !seenViewer {
			return nil
		}
		stats.viewers++
		if !currentActive {
			stats.inactiveViewers++
			return compactTimelineRanges(db, current, options.dryRun)
		}
		return nil
	}

	err := model.TimelineIndex.Iter(db, func(key, _ []byte) error {
		viewer, entry, activity, err := model.ParseTimelineIndexKey(key)
		if err != nil {
			return err
		}
		if selected != uuid.Nil && viewer != selected {
			return nil
		}
		if !seenViewer || viewer != current {
			if err := finishViewer(); err != nil {
				return err
			}
			current, currentRows, seenViewer = viewer, 0, true
			currentActive, err = model.TimelineIsActive(db, viewer, options.now)
			if err != nil {
				return err
			}
		}
		stats.indexes++
		currentRows++
		outsideTime := options.retention != model.TimelineRetentionMax && activity.Before(options.now.Add(-options.retention))
		if !currentActive || currentRows > options.maxRows || outsideTime {
			stats.deletedIndexes++
			stats.deletedPositions++
			if currentActive {
				deletes = append(deletes, deleteRow{viewer: viewer, entry: entry, activity: activity})
				if len(deletes) == batchSize {
					return flush()
				}
			}
		}
		return nil
	})
	if err != nil {
		return stats, err
	}
	if err := finishViewer(); err != nil {
		return stats, err
	}
	if err := flush(); err != nil {
		return stats, err
	}

	// Count positions before mutations so apply and dry-run report the same
	// source cardinality.
	if err := model.TimelinePosition.Iter(db, func(key, _ []byte) error {
		if len(key) != model.TimelinePosition.Prefix.Len()+2*uuid.Size {
			return fmt.Errorf("invalid TimelinePosition key length %d", len(key))
		}
		viewer, err := uuid.FromBytes(key[model.TimelinePosition.Prefix.Len() : model.TimelinePosition.Prefix.Len()+uuid.Size])
		if err != nil {
			return err
		}
		if selected != uuid.Nil && viewer != selected {
			return nil
		}
		stats.positions++
		return nil
	}); err != nil && !errors.Is(err, store.ErrNotFound) {
		return stats, err
	}

	// Reclaim position-only inactive viewers left after the Index pass. Keys are
	// ordered by viewer, so one scalar remembers the last reclaimed range.
	var lastInactive uuid.UUID
	if err := model.TimelinePosition.Iter(db, func(key, _ []byte) error {
		viewer, err := uuid.FromBytes(key[model.TimelinePosition.Prefix.Len() : model.TimelinePosition.Prefix.Len()+uuid.Size])
		if err != nil {
			return err
		}
		if selected != uuid.Nil && viewer != selected {
			return nil
		}
		active, err := model.TimelineIsActive(db, viewer, options.now)
		if err != nil {
			return err
		}
		if !active && viewer != lastInactive {
			lastInactive = viewer
			return compactTimelineRanges(db, viewer, options.dryRun)
		}
		return nil
	}); err != nil {
		return stats, err
	}
	return stats, nil
}
