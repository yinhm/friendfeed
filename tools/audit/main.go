package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func main() {
	dataPath := flag.String("data", "/srv/ffdb/data", "Path to database directory")
	flag.Parse()

	log.Printf("Opening database at %s", *dataPath)
	db, err := store.NewStore(*dataPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	log.Println("Starting Group audit checks...")

	issues := 0
	issues += len(auditAdminNonMembers(context.Background(), db))
	issues += len(auditGroupsWithoutAdmins(context.Background(), db))
	issues += len(auditOrphanedMemberships(context.Background(), db))
	issues += len(auditUnpairedMemberships(context.Background(), db))
	issues += len(auditDeletedGroupResiduals(context.Background(), db))
	issues += len(auditOrphanedFollowRequests(context.Background(), db))

	if issues == 0 {
		log.Println("✓ All audit checks passed")
	} else {
		log.Printf("✗ Found %d issue(s)", issues)
		os.Exit(1)
	}
}

// getRawProfile reads the Profile row directly, bypassing GetProfileFromUuid
// so that soft-deleted profiles are distinguishable from missing ones.
func getRawProfile(db *store.Store, profileUUID uuid.UUID) (*pb.Profile, error) {
	profile := new(pb.Profile)
	if err := model.Profile.Get(db, profileUUID.Bytes(), profile); err != nil {
		return nil, err
	}
	return profile, nil
}

// auditAdminNonMembers checks for GroupAdmin entries where the admin is not a member
func auditAdminNonMembers(ctx context.Context, db *store.Store) []string {
	log.Println("Checking for admins who are not members...")
	var issues []string

	// Scan all GroupAdmin entries
	prefix := model.NewKeyFrom(model.GroupAdmin.Prefix)
	_, err := db.ForwardScan(prefix, func(_ int, key, value []byte) error {
		// Parse GroupAdmin key: prefix + groupUUID(16) + userUUID(16)
		if len(key) < len(prefix)+32 {
			issues = append(issues, "malformed GroupAdmin key: too short")
			log.Printf("  ✗ %s", issues[len(issues)-1])
			return nil
		}

		groupUUID, err := uuid.FromBytes(key[len(prefix) : len(prefix)+16])
		if err != nil {
			issues = append(issues, "malformed GroupAdmin group UUID: "+err.Error())
			log.Printf("  ✗ %s", issues[len(issues)-1])
			return nil
		}

		userUUID, err := uuid.FromBytes(key[len(prefix)+16 : len(prefix)+32])
		if err != nil {
			issues = append(issues, "malformed GroupAdmin user UUID: "+err.Error())
			log.Printf("  ✗ %s", issues[len(issues)-1])
			return nil
		}

		// Membership is the Follow edge user -> group (see model.IsGroupMember).
		isMember, err := model.IsGroupMember(db, groupUUID, userUUID)
		if err != nil {
			issues = append(issues, "error checking membership for admin "+userUUID.String()+" in group "+groupUUID.String()+": "+err.Error())
			log.Printf("  ✗ %s", issues[len(issues)-1])
			return nil
		}

		if !isMember {
			issues = append(issues, "admin "+userUUID.String()+" is not a member of group "+groupUUID.String())
			log.Printf("  ✗ %s", issues[len(issues)-1])
		}

		return nil
	})

	if err != nil {
		issues = append(issues, "error scanning GroupAdmin: "+err.Error())
		log.Printf("  ✗ %s", issues[len(issues)-1])
	}

	if len(issues) == 0 {
		log.Println("  ✓ No orphaned admins found")
	}

	return issues
}

// auditGroupsWithoutAdmins checks for groups that have no admins
func auditGroupsWithoutAdmins(ctx context.Context, db *store.Store) []string {
	log.Println("Checking for groups without admins...")
	var issues []string

	// Scan all Profile entries with type="group"
	prefix := model.NewKeyFrom(model.Profile.Prefix)
	_, err := db.ForwardScan(prefix, func(_ int, key, value []byte) error {
		// Parse UUID from key
		if len(key) < len(prefix)+16 {
			return nil
		}
		profileUUID, err := uuid.FromBytes(key[len(prefix) : len(prefix)+16])
		if err != nil {
			return nil
		}

		profile, err := model.GetProfileFromUuid(db, profileUUID)
		if err != nil {
			return nil // Skip missing/deleted profiles
		}

		if profile.Type != "group" {
			return nil // Skip non-groups
		}

		// Check if group has at least one admin
		adminPrefix := model.NewKeyFrom(model.GroupAdmin.Prefix, profileUUID.Bytes())
		hasAdmin := false
		_, err = db.ForwardScan(adminPrefix, func(_ int, _, _ []byte) error {
			hasAdmin = true
			return &store.Error{Code: store.StopIteration}
		})
		if err != nil {
			if scanErr, ok := err.(*store.Error); !ok || scanErr.Code != store.StopIteration {
				issues = append(issues, "error scanning admins for group "+profileUUID.String()+": "+err.Error())
				log.Printf("  ✗ %s", issues[len(issues)-1])
				return nil
			}
		}

		if !hasAdmin {
			issues = append(issues, "group "+profileUUID.String()+" ("+profile.Name+") has no admins")
			log.Printf("  ✗ %s", issues[len(issues)-1])
		}

		return nil
	})

	if err != nil {
		issues = append(issues, "error scanning profiles: "+err.Error())
		log.Printf("  ✗ %s", issues[len(issues)-1])
	}

	if len(issues) == 0 {
		log.Println("  ✓ All groups have at least one admin")
	}

	return issues
}

// auditOrphanedMemberships checks for Follow edges pointing to missing or
// deleted group profiles. The raw Profile read distinguishes soft-deleted
// rows from truly absent ones.
func auditOrphanedMemberships(ctx context.Context, db *store.Store) []string {
	log.Println("Checking for orphaned memberships...")
	var issues []string

	// Scan all Follow edges
	prefix := model.NewKeyFrom(model.Follow.Prefix)
	checked := 0
	_, err := db.ForwardScan(prefix, func(_ int, key, _ []byte) error {
		checked++

		// Parse follower and target from key: prefix + followerUUID(16) + targetUUID(16)
		if len(key) < len(prefix)+32 {
			return nil // Skip malformed keys
		}

		targetStart := len(prefix) + 16
		targetUUID, err := uuid.FromBytes(key[targetStart : targetStart+16])
		if err != nil {
			return nil
		}

		// Check if target profile exists, including soft-deleted rows.
		profile, err := getRawProfile(db, targetUUID)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				issues = append(issues, "Follow edge points to non-existent profile "+targetUUID.String())
				log.Printf("  ✗ %s", issues[len(issues)-1])
			}
			return nil
		}

		// Only flag if it's a deleted group
		if profile.Type == "group" && profile.Deleted {
			issues = append(issues, "Follow edge points to deleted group "+profile.Uuid+" ("+profile.Name+")")
			log.Printf("  ✗ %s", issues[len(issues)-1])
		}

		return nil
	})

	if err != nil {
		issues = append(issues, "error scanning Follow edges: "+err.Error())
		log.Printf("  ✗ %s", issues[len(issues)-1])
	}

	if len(issues) == 0 {
		log.Printf("  ✓ No orphaned memberships found (checked %d edges)", checked)
	}

	return issues
}

// auditUnpairedMemberships checks that every membership edge to a live group
// exists in both directions: Follow(user -> group) must pair with
// Follower(group -> user) and vice versa. A one-sided edge is reported once,
// from whichever side exists.
func auditUnpairedMemberships(ctx context.Context, db *store.Store) []string {
	log.Println("Checking for unpaired memberships...")
	var issues []string

	report := func(format string, args ...interface{}) {
		issues = append(issues, fmt.Sprintf(format, args...))
		log.Printf("  ✗ %s", issues[len(issues)-1])
	}

	// isLiveGroup reports whether uuid is an existing, non-deleted group
	// profile. Missing or deleted groups are skipped here; those residuals
	// are covered by the orphaned-membership and deleted-group checks.
	isLiveGroup := func(groupUUID uuid.UUID) bool {
		profile, err := getRawProfile(db, groupUUID)
		if err != nil {
			return false
		}
		return profile.Type == "group" && !profile.Deleted
	}

	// Follow edges: prefix + userUUID(16) + groupUUID(16)
	followPrefix := model.NewKeyFrom(model.Follow.Prefix)
	_, err := db.ForwardScan(followPrefix, func(_ int, key, _ []byte) error {
		if len(key) < len(followPrefix)+32 {
			return nil
		}
		userUUID, err := uuid.FromBytes(key[len(followPrefix) : len(followPrefix)+16])
		if err != nil {
			return nil
		}
		groupUUID, err := uuid.FromBytes(key[len(followPrefix)+16 : len(followPrefix)+32])
		if err != nil {
			return nil
		}
		if !isLiveGroup(groupUUID) {
			return nil
		}
		followerKey := model.NewKeyFrom(model.Follower.Prefix, groupUUID.Bytes(), userUUID.Bytes())
		exists, err := db.Exists(followerKey)
		if err != nil {
			report("error checking Follower edge for user %s in group %s: %v", userUUID, groupUUID, err)
			return nil
		}
		if !exists {
			report("Follow edge user %s -> group %s has no matching Follower edge", userUUID, groupUUID)
		}
		return nil
	})
	if err != nil {
		report("error scanning Follow edges: %v", err)
	}

	// Follower edges: prefix + groupUUID(16) + userUUID(16)
	followerPrefix := model.NewKeyFrom(model.Follower.Prefix)
	_, err = db.ForwardScan(followerPrefix, func(_ int, key, _ []byte) error {
		if len(key) < len(followerPrefix)+32 {
			return nil
		}
		groupUUID, err := uuid.FromBytes(key[len(followerPrefix) : len(followerPrefix)+16])
		if err != nil {
			return nil
		}
		userUUID, err := uuid.FromBytes(key[len(followerPrefix)+16 : len(followerPrefix)+32])
		if err != nil {
			return nil
		}
		if !isLiveGroup(groupUUID) {
			return nil
		}
		followKey := model.NewKeyFrom(model.Follow.Prefix, userUUID.Bytes(), groupUUID.Bytes())
		exists, err := db.Exists(followKey)
		if err != nil {
			report("error checking Follow edge for user %s in group %s: %v", userUUID, groupUUID, err)
			return nil
		}
		if !exists {
			report("Follower edge group %s -> user %s has no matching Follow edge", groupUUID, userUUID)
		}
		return nil
	})
	if err != nil {
		report("error scanning Follower edges: %v", err)
	}

	if len(issues) == 0 {
		log.Println("  ✓ All group memberships are paired")
	}

	return issues
}

// auditDeletedGroupResiduals checks for GroupAdmin entries whose group
// profile is missing or soft-deleted.
func auditDeletedGroupResiduals(ctx context.Context, db *store.Store) []string {
	log.Println("Checking for deleted group residuals...")
	var issues []string

	// Scan all GroupAdmin entries
	prefix := model.NewKeyFrom(model.GroupAdmin.Prefix)
	_, err := db.ForwardScan(prefix, func(_ int, key, _ []byte) error {
		// Parse GroupAdmin key
		if len(key) < len(prefix)+32 {
			return nil
		}

		groupUUID, err := uuid.FromBytes(key[len(prefix) : len(prefix)+16])
		if err != nil {
			return nil
		}

		// Raw read distinguishes a soft-deleted group from a missing one.
		profile, err := getRawProfile(db, groupUUID)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				issues = append(issues, "GroupAdmin entry for non-existent group "+groupUUID.String())
				log.Printf("  ✗ %s", issues[len(issues)-1])
			}
			return nil
		}

		if profile.Deleted {
			issues = append(issues, "GroupAdmin entry for deleted group "+profile.Uuid+" ("+profile.Name+")")
			log.Printf("  ✗ %s", issues[len(issues)-1])
		}

		return nil
	})

	if err != nil {
		issues = append(issues, "error scanning GroupAdmin: "+err.Error())
		log.Printf("  ✗ %s", issues[len(issues)-1])
	}

	if len(issues) == 0 {
		log.Println("  ✓ No deleted group residuals found")
	}

	return issues
}

// auditOrphanedFollowRequests checks for pending follow requests whose
// target feed or requester profile is missing or soft-deleted. Request keys
// are target UUID + requester UUID.
func auditOrphanedFollowRequests(ctx context.Context, db *store.Store) []string {
	log.Println("Checking for orphaned follow requests...")
	var issues []string

	prefix := model.NewKeyFrom(model.FollowRequest.Prefix)
	report := func(format string, args ...interface{}) {
		issues = append(issues, fmt.Sprintf(format, args...))
		log.Printf("  ✗ %s", issues[len(issues)-1])
	}
	_, err := db.ForwardScan(prefix, func(_ int, key, _ []byte) error {
		if len(key) != len(prefix)+32 {
			report("malformed FollowRequest key: length %d", len(key)-len(prefix))
			return nil
		}
		target, err := uuid.FromBytes(key[len(prefix) : len(prefix)+16])
		if err != nil {
			report("malformed FollowRequest target UUID: %v", err)
			return nil
		}
		requester, err := uuid.FromBytes(key[len(prefix)+16:])
		if err != nil {
			report("malformed FollowRequest requester UUID: %v", err)
			return nil
		}

		targetProfile, err := getRawProfile(db, target)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				report("follow request against non-existent feed %s", target)
			}
		} else if targetProfile.Deleted {
			report("follow request against deleted feed %s (%s)", targetProfile.Uuid, targetProfile.Name)
		}

		requesterProfile, err := getRawProfile(db, requester)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				report("follow request from non-existent user %s", requester)
			}
		} else if requesterProfile.Deleted {
			report("follow request from deleted user %s (%s)", requesterProfile.Uuid, requesterProfile.Name)
		}
		return nil
	})
	if err != nil {
		report("error scanning FollowRequest: %v", err)
	}

	if len(issues) == 0 {
		log.Println("  ✓ No orphaned follow requests found")
	}
	return issues
}
