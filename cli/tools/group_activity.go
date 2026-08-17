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
		if len(rows) == 0 && len(current) == 0 {
			// Not a member of any Group and no stale ranking to clear;
			// skip rather than materialize an empty key.
			return nil
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

	// Default scope: only users with an OAuth login identity. The
	// materialized ranking only serves logged-in sidebars, so imported
	// ghost profiles are never worth rebuilding.
	logins, err := oauthProfileUUIDs(db)
	if err != nil {
		return stats, err
	}
	for _, user := range logins {
		profile, err := model.GetProfileFromUuid(db, user)
		if errors.Is(err, model.ErrProfileDeleted) || errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			return stats, err
		}
		if err := rebuild(profile); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

// oauthProfileUUIDs returns the distinct Profile UUIDs behind all OAuth
// identities; one user may hold several identities (e.g. Twitter + Google).
func oauthProfileUUIDs(db *store.Store) ([]uuid.UUID, error) {
	seen := make(map[uuid.UUID]struct{})
	var users []uuid.UUID
	err := model.OAuth.Iter(db, func(_, raw []byte) error {
		oauth := new(pb.OAuthUser)
		if err := proto.Unmarshal(raw, oauth); err != nil {
			return err
		}
		user, err := uuid.FromString(oauth.Uuid)
		if err != nil || user == uuid.Nil {
			return nil
		}
		if _, ok := seen[user]; !ok {
			seen[user] = struct{}{}
			users = append(users, user)
		}
		return nil
	})
	return users, err
}
