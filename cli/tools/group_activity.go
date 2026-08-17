package main

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type groupActivityRebuildStats struct {
	users, groups, changed int
}

func rebuildGroupActivity(db *store.Store, userID string, dryRun bool) (groupActivityRebuildStats, error) {
	stats := groupActivityRebuildStats{}
	rebuild := func(profile *pb.Profile) error {
		if profile == nil || profile.Deleted || profile.Type != "user" {
			return nil
		}
		user, err := uuid.FromString(profile.Uuid)
		if err != nil || user == uuid.Nil {
			return fmt.Errorf("profile %q has invalid UUID", profile.Id)
		}
		rows, err := model.RebuildGroupActivityForUser(db, user)
		if err != nil {
			return fmt.Errorf("rebuild Group activity for %s: %w", profile.Id, err)
		}
		current, err := model.GetGroupActivity(db, user)
		if errors.Is(err, store.ErrNotFound) {
			current = nil
		} else if err != nil {
			return err
		}
		stats.users++
		stats.groups += len(rows)
		if reflect.DeepEqual(current, rows) {
			return nil
		}
		stats.changed++
		if !dryRun {
			return model.ReplaceGroupActivity(db, user, rows)
		}
		return nil
	}

	if userID != "" {
		profile, err := model.GetProfileFromUserId(db, userID)
		if err != nil {
			return stats, err
		}
		return stats, rebuild(profile)
	}
	err := model.Profile.Iter(db, func(_ []byte, raw []byte) error {
		profile := new(pb.Profile)
		if err := proto.Unmarshal(raw, profile); err != nil {
			return err
		}
		return rebuild(profile)
	})
	return stats, err
}
