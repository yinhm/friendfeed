package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type groupIndexRebuildStats struct {
	profiles int
	indexed  int
	changed  int
	stale    int
}

func rebuiltGroupIndexActivity(db *store.Store, group uuid.UUID) (time.Time, error) {
	activity, ok, err := model.LatestGroupEntryActivity(db, group)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return time.Unix(0, 0).UTC(), nil
	}
	return activity, nil
}

func rebuildGroupIndex(db *store.Store, groupID string, dryRun bool) (groupIndexRebuildStats, error) {
	stats := groupIndexRebuildStats{}
	rebuild := func(profile *pb.Profile, write, countStats, tableCleared bool) error {
		if profile == nil || profile.Deleted || profile.Type != "group" {
			return nil
		}
		group, err := uuid.FromString(profile.Uuid)
		if err != nil || group == uuid.Nil {
			return fmt.Errorf("Group %q has invalid UUID", profile.Id)
		}
		want, err := rebuiltGroupIndexActivity(db, group)
		if err != nil {
			return fmt.Errorf("rebuild Group %s: %w", profile.Id, err)
		}
		if tableCleared {
			return db.ApplyBatch(func(batch *pebble.Batch) error {
				return model.StageCreateGroupIndex(batch, group, want)
			})
		}
		got, count, err := model.GroupIndexActivity(db, group)
		if err != nil {
			return err
		}
		if countStats {
			stats.profiles++
		}
		if count == 1 && got.Equal(want) {
			if countStats {
				stats.indexed++
			}
			return nil
		}
		if countStats {
			stats.changed++
		}
		if !write {
			return nil
		}
		return db.ApplyBatch(func(batch *pebble.Batch) error {
			// Remove every stale/duplicate position for this one Group before
			// writing its rebuilt canonical row.
			iter, err := db.NewIterator(model.GroupIndex.Prefix)
			if err != nil {
				return err
			}
			defer iter.Close()
			for iter.First(); iter.Valid(); iter.Next() {
				indexed, _, err := model.ParseGroupIndexKey(iter.UnsafeKey())
				if err != nil {
					return err
				}
				if indexed == group {
					if countStats {
						stats.stale++
					}
					if err := batch.Delete(iter.Key(), nil); err != nil {
						return err
					}
				}
			}
			if err := iter.Error(); err != nil {
				return err
			}
			return model.StageCreateGroupIndex(batch, group, want)
		})
	}

	if groupID != "" {
		profile, err := model.GetProfileFromUserId(db, groupID)
		if err != nil {
			return stats, err
		}
		if profile.Type != "group" || profile.Deleted {
			return stats, errors.New("target is not a live Group")
		}
		return stats, rebuild(profile, !dryRun, true, false)
	}

	iterateProfiles := func(fn func(*pb.Profile) error) error {
		return model.Profile.Iter(db, func(_ []byte, raw []byte) error {
			profile := new(pb.Profile)
			if err := proto.Unmarshal(raw, profile); err != nil {
				return err
			}
			return fn(profile)
		})
	}
	// Compare before clearing so apply reports the same indexed/changed
	// population as dry-run. This extra Profile pass is streaming and bounded.
	if err := iterateProfiles(func(profile *pb.Profile) error {
		return rebuild(profile, false, true, false)
	}); err != nil {
		return stats, err
	}
	if dryRun {
		return stats, nil
	}
	{
		upper := store.KeyUpperBound(model.GroupIndex.Prefix)
		if err := db.ApplyBatch(func(batch *pebble.Batch) error {
			return batch.DeleteRange(model.GroupIndex.Prefix, upper, nil)
		}); err != nil {
			return stats, err
		}
	}
	if err := iterateProfiles(func(profile *pb.Profile) error {
		return rebuild(profile, true, false, true)
	}); err != nil {
		return stats, err
	}
	return stats, nil
}
