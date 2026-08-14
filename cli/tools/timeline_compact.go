package main

import (
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
	coldRows  int
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

func timelineIndexHasRows(db *store.Store, viewer uuid.UUID) (bool, error) {
	iter, err := db.NewIterator(model.TimelineIndexPrefix(viewer))
	if err != nil {
		return false, err
	}
	defer iter.Close()
	iter.First()
	if err := iter.Error(); err != nil {
		return false, err
	}
	return iter.Valid(), nil
}

func compactTimelines(db *store.Store, options timelineCompactOptions) (timelineCompactStats, error) {
	stats := timelineCompactStats{}
	maxRowsExplicit := options.maxRows > 0
	if !maxRowsExplicit {
		options.maxRows = model.TimelineMaxEntries
	}
	if options.coldRows <= 0 {
		options.coldRows = model.TimelineColdEntries
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

	// Count positions before the Index pass starts deleting paired rows. A
	// position-only inactive viewer is fully reclaimable and counted here;
	// paired row deletions are counted later from the ordered Index scan.
	var positionViewer uuid.UUID
	positionStarted := false
	positionDeleteAll := false
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
		if !positionStarted || viewer != positionViewer {
			positionStarted = true
			positionViewer = viewer
			// The public timeline has no TimelineState and never decays.
			active := model.IsPublicTimeline(viewer)
			if !active {
				var activeErr error
				active, activeErr = model.TimelineIsActive(db, viewer, options.now)
				if activeErr != nil {
					return activeErr
				}
			}
			positionDeleteAll = false
			if !active {
				hasIndex, indexErr := timelineIndexHasRows(db, viewer)
				if indexErr != nil {
					return indexErr
				}
				positionDeleteAll = !hasIndex
			}
		}
		stats.positions++
		if positionDeleteAll {
			stats.deletedPositions++
		}
		return nil
	}); err != nil {
		return stats, err
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
			currentActive = model.IsPublicTimeline(viewer)
			if !currentActive {
				currentActive, err = model.TimelineIsActive(db, viewer, options.now)
				if err != nil {
					return err
				}
			}
		}
		stats.indexes++
		currentRows++
		rowLimit := options.maxRows
		if model.IsPublicTimeline(viewer) && !maxRowsExplicit {
			rowLimit = model.PublicTimelineMaxEntries
		}
		if !currentActive {
			rowLimit = options.coldRows
		}
		outsideTime := options.retention != model.TimelineRetentionMax && activity.Before(options.now.Add(-options.retention))
		if currentRows > rowLimit || outsideTime {
			stats.deletedIndexes++
			stats.deletedPositions++
			deletes = append(deletes, deleteRow{viewer: viewer, entry: entry, activity: activity})
			if len(deletes) == batchSize {
				return flush()
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

	// Reclaim an inactive viewer only when it has positions but no Index rows at
	// all. Mixed Index/Position drift remains audit/rebuild territory; deleting
	// the whole range would discard the retained cold cache.
	var lastInactive uuid.UUID
	var checkedViewer uuid.UUID
	checkedActive := false
	checkedHasIndex := false
	if err := model.TimelinePosition.Iter(db, func(key, _ []byte) error {
		viewer, err := uuid.FromBytes(key[model.TimelinePosition.Prefix.Len() : model.TimelinePosition.Prefix.Len()+uuid.Size])
		if err != nil {
			return err
		}
		if selected != uuid.Nil && viewer != selected {
			return nil
		}
		if viewer != checkedViewer {
			checkedViewer = viewer
			checkedActive = model.IsPublicTimeline(viewer)
			if !checkedActive {
				checkedActive, err = model.TimelineIsActive(db, viewer, options.now)
				if err != nil {
					return err
				}
			}
			checkedHasIndex = false
			if !checkedActive {
				checkedHasIndex, err = timelineIndexHasRows(db, viewer)
				if err != nil {
					return err
				}
			}
		}
		if !checkedActive && !checkedHasIndex && viewer != lastInactive {
			lastInactive = viewer
			return compactTimelineRanges(db, viewer, options.dryRun)
		}
		return nil
	}); err != nil {
		return stats, err
	}
	return stats, nil
}
