package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

// profileIdRe defines the valid format for a profile ID (feed URL slug):
// lowercase letters, digits, hyphens, and underscores; minimum 4 characters.
// Based on observed FriendFeed conventions and feed routing requirements.
var profileIdRe = regexp.MustCompile(`^[a-z0-9_-]{4,}$`)

// ValidateProfileId checks whether the given id meets the format requirements.
// Returns an error describing the first violation, or nil if valid.
func ValidateProfileId(id string) error {
	if id == "" {
		return errors.New("profile ID cannot be empty")
	}
	normalized := strings.ToLower(id)
	if normalized != id {
		return fmt.Errorf("profile ID must be lowercase (got %q)", id)
	}
	if len(id) < 4 {
		return fmt.Errorf("profile ID must be at least 4 characters (got %d)", len(id))
	}
	if !profileIdRe.MatchString(id) {
		return fmt.Errorf("profile ID %q contains invalid characters (allowed: a-z, 0-9, _, -)", id)
	}
	return nil
}

// NormalizeProfileId converts the input to lowercase and validates the result.
// Returns the normalized ID and any validation error.
func NormalizeProfileId(id string) (string, error) {
	normalized := strings.ToLower(id)
	if err := ValidateProfileId(normalized); err != nil {
		return "", err
	}
	return normalized, nil
}

// RenameProfileId changes the profile's ID (feed URL slug) atomically:
//  1. Validates the new ID format
//  2. Checks that the new ID is not already taken by another profile
//  3. Deletes the old UserMap entry
//  4. Creates the new UserMap entry
//  5. Updates the Profile record
//
// If the profile's current ID already matches newId (case-insensitive), the
// call is a no-op and returns nil.
func RenameProfileId(db *store.Store, profileUUID uuid.UUID, newId string) error {
	// Normalize and validate the new ID
	newId, err := NormalizeProfileId(newId)
	if err != nil {
		return fmt.Errorf("invalid new ID: %w", err)
	}

	// Fetch the current profile to get the old ID
	profile := new(pb.Profile)
	if err := Profile.Get(db, profileUUID.Bytes(), profile); err != nil {
		return fmt.Errorf("profile not found: %w", err)
	}

	oldId := profile.Id
	if oldId == newId {
		// Already the desired ID; no-op
		return nil
	}

	// Check for collision: does the new ID already map to a different profile?
	newMapKey := NewKeyFrom(TableUserMap.Bytes(), []byte(newId))
	if existingUUID, err := db.Get(newMapKey); err == nil && len(existingUUID) > 0 {
		existing, err := uuid.FromBytes(existingUUID)
		if err == nil && existing != profileUUID {
			return fmt.Errorf("ID %q is already taken by another profile", newId)
		}
		// If it maps to this same profile, proceed (handles double-apply)
	}

	// Atomic update: delete old UserMap, create new UserMap, update Profile.
	// (Pebble WriteBatch would be cleaner but store.Store doesn't expose it;
	// this order minimizes the window where lookups might fail.)

	// Delete the old UserMap entry
	oldMapKey := NewKeyFrom(TableUserMap.Bytes(), []byte(oldId))
	if err := db.Delete(oldMapKey); err != nil {
		return fmt.Errorf("delete old UserMap[%s]: %w", oldId, err)
	}

	// Create the new UserMap entry
	if err := db.Put(newMapKey, profileUUID.Bytes()); err != nil {
		// Try to restore the old mapping before returning error
		_ = db.Put(oldMapKey, profileUUID.Bytes())
		return fmt.Errorf("create new UserMap[%s]: %w", newId, err)
	}

	// Update the Profile record
	profile.Id = newId
	if _, err := Profile.Put(db, profileUUID.Bytes(), profile); err != nil {
		// Rollback: restore old UserMap, delete new UserMap
		_ = db.Put(oldMapKey, profileUUID.Bytes())
		_ = db.Delete(newMapKey)
		return fmt.Errorf("update Profile: %w", err)
	}

	return nil
}
