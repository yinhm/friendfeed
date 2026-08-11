package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
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
//  3. Records old ID -> stable UUID in UserRenameMap
//  4. Deletes the old UserMap entry
//  5. Creates the new UserMap entry
//  6. Updates the Profile record
//
// If the profile's current ID exactly matches the normalized newId and its
// existing UserMap entry still points to profileUUID, the call is a no-op.
// A missing, malformed, or conflicting old mapping is reported even when the
// requested ID text is unchanged, because that state is not a valid no-op.
func RenameProfileId(db *store.Store, profileUUID uuid.UUID, newId string) error {
	if profileUUID == uuid.Nil {
		return errors.New("profile UUID is invalid")
	}

	// Normalize and validate the new ID
	newId, err := NormalizeProfileId(newId)
	if err != nil {
		return fmt.Errorf("invalid new ID: %w", err)
	}

	return db.ApplyBatch(func(batch *pebble.Batch) error {
		// The profile read, collision check and commit are serialized with
		// other rename batches on this Store.
		profile := new(pb.Profile)
		if err := Profile.Get(db, profileUUID.Bytes(), profile); err != nil {
			return fmt.Errorf("profile not found: %w", err)
		}

		oldId := profile.Id
		oldMapKey := NewKeyFrom(TableUserMap.Bytes(), []byte(oldId))
		oldMappedUUID, err := db.Get(oldMapKey)
		if err != nil {
			return fmt.Errorf("read old UserMap[%s]: %w", oldId, err)
		}
		if len(oldMappedUUID) == 0 {
			return fmt.Errorf("old UserMap[%s] is missing", oldId)
		}
		oldMappedProfile, err := uuid.FromBytes(oldMappedUUID)
		if err != nil {
			return fmt.Errorf("decode old UserMap[%s]: %w", oldId, err)
		}
		if oldMappedProfile != profileUUID {
			return fmt.Errorf("old UserMap[%s] belongs to another profile", oldId)
		}
		if oldId == newId {
			return nil
		}

		previousID, err := findPreviousIDByProfileUUID(db, profileUUID)
		if err == nil {
			return fmt.Errorf("profile ID was already renamed from %q", previousID)
		}
		if !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("check previous rename for profile %s: %w", profileUUID, err)
		}
		if reservedUUID, err := FindProfileRenameByOldId(db, newId); err == nil {
			return fmt.Errorf("ID %q is reserved by a previous rename of profile %s", newId, reservedUUID)
		} else if !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("check renamed ID %q: %w", newId, err)
		}

		newMapKey := NewKeyFrom(TableUserMap.Bytes(), []byte(newId))
		existingUUID, err := db.Get(newMapKey)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("read UserMap[%s]: %w", newId, err)
		}
		if len(existingUUID) > 0 {
			existing, err := uuid.FromBytes(existingUUID)
			if err != nil {
				return fmt.Errorf("decode UserMap[%s]: %w", newId, err)
			}
			if existing != profileUUID {
				return fmt.Errorf("ID %q is already taken by another profile", newId)
			}
		}

		profile.Id = newId
		encodedProfile, err := proto.Marshal(profile)
		if err != nil {
			return fmt.Errorf("encode Profile: %w", err)
		}

		profileKey := Profile.PrefixAppend(profileUUID.Bytes())
		if err := batch.Delete(oldMapKey, nil); err != nil {
			return fmt.Errorf("delete old UserMap[%s]: %w", oldId, err)
		}
		if err := batch.Set(newMapKey, profileUUID.Bytes(), nil); err != nil {
			return fmt.Errorf("create new UserMap[%s]: %w", newId, err)
		}
		if err := batch.Set(profileKey, encodedProfile, nil); err != nil {
			return fmt.Errorf("update Profile: %w", err)
		}
		renameKey := UserRenameMap.PrefixAppend([]byte(oldId))
		if err := batch.Set(renameKey, profileUUID.Bytes(), nil); err != nil {
			return fmt.Errorf("create UserRenameMap[%s]: %w", oldId, err)
		}
		return nil
	})
}

// FindProfileRenameByOldId returns the stable profile UUID recorded for oldID.
// This is the normal read path for resolving /feed/<old-id>.
func FindProfileRenameByOldId(db *store.Store, oldID string) (uuid.UUID, error) {
	raw, err := UserRenameMap.GetRaw(db, []byte(oldID))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, err
	}
	profileUUID, err := uuid.FromBytes(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("decode UserRenameMap[%s]: %w", oldID, err)
	}
	if profileUUID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("decode UserRenameMap[%s]: zero UUID", oldID)
	}
	return profileUUID, nil
}

// findPreviousIDByProfileUUID scans the deliberately small metadata table to
// enforce the one-active-rename-per-profile rule. Redirect lookup itself is a
// direct old-ID key lookup.
func findPreviousIDByProfileUUID(db *store.Store, profileUUID uuid.UUID) (string, error) {
	var previousID string
	err := UserRenameMap.Iter(db, func(key, raw []byte) error {
		mappedUUID, err := uuid.FromBytes(raw)
		if err != nil {
			return fmt.Errorf("decode UserRenameMap value for key %x: %w", key, err)
		}
		if mappedUUID != profileUUID {
			return nil
		}
		oldID := string(UserRenameMap.PrefixRemove(store.Key(key)))
		if oldID == "" {
			return errors.New("UserRenameMap contains an empty old ID")
		}
		if previousID != "" && previousID != oldID {
			return fmt.Errorf("profile %s has multiple previous IDs", profileUUID)
		}
		previousID = oldID
		return nil
	})
	if err != nil {
		return "", err
	}
	if previousID == "" {
		return "", ErrNotFound
	}
	return previousID, nil
}

// GetProfileFromRenameId resolves an old profile ID to the profile's current
// canonical record.
func GetProfileFromRenameId(db *store.Store, oldID string) (*pb.Profile, error) {
	profileUUID, err := FindProfileRenameByOldId(db, oldID)
	if err != nil {
		return nil, err
	}
	return GetProfileFromUuid(db, profileUUID)
}
