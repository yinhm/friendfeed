package main

import (
	"context"
	"flag"
	"log"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
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
	issues += auditAdminNonMembers(context.Background(), db)
	issues += auditGroupsWithoutAdmins(context.Background(), db)
	issues += auditOrphanedMemberships(context.Background(), db)
	issues += auditDeletedGroupResiduals(context.Background(), db)

	if issues == 0 {
		log.Println("✓ All audit checks passed")
	} else {
		log.Printf("✗ Found %d issue(s)", issues)
	}
}

// auditAdminNonMembers checks for GroupAdmin entries where the admin is not a member
func auditAdminNonMembers(ctx context.Context, db *store.Store) int {
	log.Println("Checking for admins who are not members...")
	issues := 0

	// Scan all GroupAdmin entries
	prefix := model.NewKeyFrom(model.GroupAdmin.Prefix)
	_, err := db.ForwardScan(prefix, func(_ int, key, value []byte) error {
		// Parse GroupAdmin key: prefix + groupUUID(16) + userUUID(16)
		if len(key) < len(prefix)+32 {
			log.Printf("  ✗ Malformed GroupAdmin key: too short")
			issues++
			return nil
		}

		groupUUID, err := uuid.FromBytes(key[len(prefix) : len(prefix)+16])
		if err != nil {
			log.Printf("  ✗ Malformed GroupAdmin group UUID: %v", err)
			issues++
			return nil
		}

		userUUID, err := uuid.FromBytes(key[len(prefix)+16 : len(prefix)+32])
		if err != nil {
			log.Printf("  ✗ Malformed GroupAdmin user UUID: %v", err)
			issues++
			return nil
		}

		// Check if corresponding Follow edge exists (group -> user)
		followKey := model.NewKeyFrom(model.Follow.Prefix, groupUUID.Bytes(), userUUID.Bytes())
		exists, err := db.Exists(followKey)
		if err != nil {
			log.Printf("  ✗ Error checking membership for admin %s in group %s: %v",
				userUUID, groupUUID, err)
			issues++
			return nil
		}

		if !exists {
			log.Printf("  ✗ Admin %s is not a member of group %s", userUUID, groupUUID)
			issues++
		}

		return nil
	})

	if err != nil {
		log.Printf("  ✗ Error scanning GroupAdmin: %v", err)
		issues++
	}

	if issues == 0 {
		log.Println("  ✓ No orphaned admins found")
	}

	return issues
}

// auditGroupsWithoutAdmins checks for groups that have no admins
func auditGroupsWithoutAdmins(ctx context.Context, db *store.Store) int {
	log.Println("Checking for groups without admins...")
	issues := 0

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
				log.Printf("  ✗ Error scanning admins for group %s: %v", profileUUID, err)
				issues++
				return nil
			}
		}

		if !hasAdmin {
			log.Printf("  ✗ Group %s (%s) has no admins", profileUUID, profile.Name)
			issues++
		}

		return nil
	})

	if err != nil {
		log.Printf("  ✗ Error scanning profiles: %v", err)
		issues++
	}

	if issues == 0 {
		log.Println("  ✓ All groups have at least one admin")
	}

	return issues
}

// auditOrphanedMemberships checks for Follow edges to deleted groups
func auditOrphanedMemberships(ctx context.Context, db *store.Store) int {
	log.Println("Checking for orphaned memberships...")
	issues := 0

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

		// Check if target profile exists
		profile, err := model.GetProfileFromUuid(db, targetUUID)
		if err != nil {
			if err == store.ErrNotFound {
				log.Printf("  ✗ Follow edge points to non-existent profile %s", targetUUID)
				issues++
			}
			return nil
		}

		// Only flag if it's a deleted group
		if profile.Type == "group" && profile.Deleted {
			log.Printf("  ✗ Follow edge points to deleted group %s (%s)",
				profile.Uuid, profile.Name)
			issues++
		}

		return nil
	})

	if err != nil {
		log.Printf("  ✗ Error scanning Follow edges: %v", err)
		issues++
	}

	if issues == 0 {
		log.Printf("  ✓ No orphaned memberships found (checked %d edges)", checked)
	}

	return issues
}

// auditDeletedGroupResiduals checks for GroupAdmin entries for deleted groups
func auditDeletedGroupResiduals(ctx context.Context, db *store.Store) int {
	log.Println("Checking for deleted group residuals...")
	issues := 0

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

		// Check if group profile exists and is not deleted
		profile, err := model.GetProfileFromUuid(db, groupUUID)
		if err != nil {
			if err == store.ErrNotFound {
				log.Printf("  ✗ GroupAdmin entry for non-existent group %s", groupUUID)
				issues++
			}
			return nil
		}

		if profile.Deleted {
			log.Printf("  ✗ GroupAdmin entry for deleted group %s (%s)",
				profile.Uuid, profile.Name)
			issues++
		}

		return nil
	})

	if err != nil {
		log.Printf("  ✗ Error scanning GroupAdmin: %v", err)
		issues++
	}

	if issues == 0 {
		log.Println("  ✓ No deleted group residuals found")
	}

	return issues
}
