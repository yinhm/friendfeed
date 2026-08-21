package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type groupAdminBackfillStats struct {
	groupsScanned  int
	alreadyAdmined int
	backfilled     int
	adminsWritten  int
	unresolved     int
	orphaned       int
}

// resolveLegacyAdmin resolves one legacy Feedinfo.Admins snapshot to a live
// user Profile. The snapshot predates stable UUIDs, so it may carry only the
// profile ID from that era; resolution tries the stored UUID first, then the
// current UserMap id, then the UserRenameMap so a renamed account still
// resolves to its current profile. Deleted, missing, and non-user profiles
// are skipped rather than migrated, per the explicit migration rule.
func resolveLegacyAdmin(db *store.Store, snapshot *pb.Profile) (*pb.Profile, bool) {
	if snapshot == nil {
		return nil, false
	}
	if snapshot.Uuid != "" {
		if profileUUID, err := uuid.FromString(snapshot.Uuid); err == nil {
			if profile, err := model.GetProfileFromUuid(db, profileUUID); err == nil && profile.Type == "user" {
				return profile, true
			}
		}
	}
	if snapshot.Id == "" {
		return nil, false
	}
	if profile, err := model.GetProfileFromUserId(db, snapshot.Id); err == nil && profile.Type == "user" {
		return profile, true
	}
	if profile, err := model.GetProfileFromRenameId(db, snapshot.Id); err == nil && profile.Type == "user" {
		return profile, true
	}
	return nil, false
}

// backfillGroupAdmins migrates legacy Feedinfo.Admins snapshots into
// TableGroupAdmin rows for Groups that have no admin at all. Groups created
// after TableGroupAdmin shipped already carry their role rows and are left
// untouched; Groups whose snapshots all fail to resolve are reported as
// orphaned instead of guessing an owner.
//
// Every migrated admin also gets the Follow/Follower membership edges, which
// the docs/group.md invariant GroupAdmin => Follow requires and which private
// Group reads authorize against. The writes for one Group commit in a single
// batch. The scan streams the Profile table; only one Group's (small) admin
// list is held at a time.
//
// The per-user Group activity index is not rebuilt here: it only affects
// sidebar ranking, and rebuild_group_activity recomputes it if needed.
func backfillGroupAdmins(db *store.Store, dryRun bool) (groupAdminBackfillStats, error) {
	stats := groupAdminBackfillStats{}
	err := model.Profile.Iter(db, func(key, raw []byte) error {
		group := new(pb.Profile)
		if err := proto.Unmarshal(raw, group); err != nil {
			return fmt.Errorf("decode profile at %x: %w", key, err)
		}
		if group.Type != "group" || group.Deleted {
			return nil
		}
		stats.groupsScanned++
		groupUUID, err := uuid.FromString(group.Uuid)
		if err != nil {
			return fmt.Errorf("Group %q has invalid UUID: %w", group.Id, err)
		}
		adminCount, err := model.CountGroupAdmins(db, groupUUID)
		if err != nil {
			return fmt.Errorf("count admins of Group %q: %w", group.Id, err)
		}
		if adminCount > 0 {
			stats.alreadyAdmined++
			return nil
		}

		info, err := model.GetFeedinfo(db, group.Uuid)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return fmt.Errorf("read legacy feedinfo of Group %q: %w", group.Id, err)
		}

		seen := make(map[uuid.UUID]struct{})
		var admins []uuid.UUID
		if info != nil {
			for _, snapshot := range info.Admins {
				profile, ok := resolveLegacyAdmin(db, snapshot)
				if !ok {
					stats.unresolved++
					continue
				}
				profileUUID, err := uuid.FromString(profile.Uuid)
				if err != nil {
					stats.unresolved++
					continue
				}
				if _, dup := seen[profileUUID]; dup {
					continue
				}
				seen[profileUUID] = struct{}{}
				admins = append(admins, profileUUID)
			}
		}
		if len(admins) == 0 {
			stats.orphaned++
			return nil
		}

		stats.backfilled++
		stats.adminsWritten += len(admins)
		if dryRun {
			return nil
		}
		return db.ApplyBatch(func(batch *pebble.Batch) error {
			for _, admin := range admins {
				followKey := model.NewKeyFrom(model.Follow.Prefix, admin.Bytes(), groupUUID.Bytes())
				if err := batch.Set(followKey, []byte("1"), nil); err != nil {
					return fmt.Errorf("stage Follow %s -> %s: %w", admin, groupUUID, err)
				}
				followerKey := model.NewKeyFrom(model.Follower.Prefix, groupUUID.Bytes(), admin.Bytes())
				if err := batch.Set(followerKey, []byte("1"), nil); err != nil {
					return fmt.Errorf("stage Follower %s -> %s: %w", groupUUID, admin, err)
				}
				adminKey, err := model.GroupAdminKey(groupUUID, admin)
				if err != nil {
					return err
				}
				if err := batch.Set(adminKey, nil, nil); err != nil {
					return fmt.Errorf("stage GroupAdmin %s -> %s: %w", admin, groupUUID, err)
				}
			}
			return nil
		})
	})
	return stats, err
}

func runBackfillGroupAdminsCommand(ndb *store.Store) {
	stats, err := backfillGroupAdmins(ndb, dryRun)
	if err != nil {
		log.Fatal(err)
	}
	if !dryRun {
		if err := ndb.Flush(); err != nil {
			log.Fatalf("flush database: %v", err)
		}
	}
	log.Printf("Group admin backfill summary: %d groups scanned, %d already had admins, %d backfilled, %d admins written, %d unresolved snapshots, %d orphaned, dry-run=%t",
		stats.groupsScanned, stats.alreadyAdmined, stats.backfilled, stats.adminsWritten, stats.unresolved, stats.orphaned, dryRun)
}
