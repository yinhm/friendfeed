package main

import (
	"errors"
	"fmt"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type feedArchiveRebuildStats struct {
	feeds   int
	entries int64
	changed int
}

func rebuildOneFeedArchive(db *store.Store, profile *pb.Profile, dryRun bool, stats *feedArchiveRebuildStats) error {
	if profile == nil || profile.Deleted || (profile.Type != "user" && profile.Type != "group") {
		return nil
	}
	feed, err := uuid.FromString(profile.Uuid)
	if err != nil || feed == uuid.Nil {
		return fmt.Errorf("profile %q has invalid UUID", profile.Id)
	}
	built, err := model.BuildFeedArchive(db, feed)
	if err != nil {
		return fmt.Errorf("build Feed archive for %s: %w", profile.Id, err)
	}
	existing, getErr := model.GetFeedArchive(db, feed)
	changed := getErr != nil || !proto.Equal(existing, built)
	if getErr != nil && !errors.Is(getErr, store.ErrNotFound) {
		// A corrupt/old derived snapshot is replaced just like a missing one.
		changed = true
	}
	// A rebuild also publishes freshness. Clear a dirty marker even when the
	// rebuilt snapshot happens to be byte-for-byte equivalent.
	if _, dirtyErr := model.FeedArchiveDirtySince(db, feed); dirtyErr == nil || !errors.Is(dirtyErr, store.ErrNotFound) {
		changed = true
	}
	if changed && !dryRun {
		if err := model.PutFeedArchive(db, feed, built); err != nil {
			return fmt.Errorf("write Feed archive for %s: %w", profile.Id, err)
		}
	}
	stats.feeds++
	stats.entries += built.EntryCount
	if changed {
		stats.changed++
	}
	return nil
}

func rebuildFeedArchives(db *store.Store, user string, dryRun bool) (feedArchiveRebuildStats, error) {
	stats := feedArchiveRebuildStats{}
	if user != "" {
		profile, err := model.GetProfileFromUserId(db, user)
		if err != nil {
			return stats, err
		}
		err = rebuildOneFeedArchive(db, profile, dryRun, &stats)
		return stats, err
	}
	err := model.Profile.Iter(db, func(_, value []byte) error {
		profile := new(pb.Profile)
		if err := proto.Unmarshal(value, profile); err != nil {
			return err
		}
		return rebuildOneFeedArchive(db, profile, dryRun, &stats)
	})
	return stats, err
}
