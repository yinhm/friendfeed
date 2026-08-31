package model

import (
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

// ErrProfileDeleted is returned by GetProfileFromUuid when the profile
// exists but is marked deleted. Callers should treat it like not-found.
var ErrProfileDeleted = errors.New("profile deleted")

// ErrProfileIdTaken is returned when a write would map a profile ID to a
// different profile than the one already holding it in UserMap.
var ErrProfileIdTaken = errors.New("profile ID is already taken")

// ErrProfileIdReserved is returned when a write targets an ID held by a
// previous rename (UserRenameMap). Like ErrProfileIdTaken it marks the ID as
// unavailable, so generated-ID allocation retries on either one.
var ErrProfileIdReserved = errors.New("profile ID is reserved by a previous rename")

// profileIdAlphabet is the character set for generated profile IDs. Together
// with the literal "ff-" prefix it keeps generated IDs inside the
// ValidateProfileId charset.
const profileIdAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// generatedProfileIdLength is the number of random characters after the
// "ff-" prefix. 36^8 combinations make collisions vanishingly rare;
// GetOrCreateProfileFromOAuthUser still verifies availability before writing.
const generatedProfileIdLength = 8

// generateProfileId returns a random profile ID of the form "ff-xxxxxxxx"
// that satisfies ValidateProfileId.
func generateProfileId() (string, error) {
	buf := make([]byte, generatedProfileIdLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate profile ID: %w", err)
	}
	for i, b := range buf {
		buf[i] = profileIdAlphabet[int(b)%len(profileIdAlphabet)]
	}
	return "ff-" + string(buf), nil
}

// GetOrCreateProfileFromOAuthUser returns the profile for authinfo.Uuid,
// creating it with a system-generated ID when it does not exist yet.
// created reports whether this call created the profile.
//
// New profiles get a system-generated ID ("ff-" + random). The provider
// display name is kept in Name only: it is not unique and often not a
// valid feed slug. Users pick a meaningful ID later through the profile
// page rename flow.
func GetOrCreateProfileFromOAuthUser(db *store.Store, authinfo *pb.OAuthUser) (profile *pb.Profile, created bool, err error) {
	for attempt := 0; attempt < 10; attempt++ {
		id, err := generateProfileId()
		if err != nil {
			return nil, false, err
		}
		candidate := &pb.Profile{
			Uuid:        authinfo.Uuid,
			Id:          id,
			Name:        authinfo.NickName,
			Type:        "user",
			Private:     false,
			Picture:     authinfo.AvatarUrl,
			Description: authinfo.Description,
		}
		profile, created, err := createProfileIfAbsent(db, candidate)
		if err != nil {
			if errors.Is(err, ErrProfileIdTaken) || errors.Is(err, ErrProfileIdReserved) {
				continue
			}
			return nil, false, err
		}
		return profile, created, nil
	}
	return nil, false, errors.New("could not allocate a unique profile ID")
}

// createProfileIfAbsent stages candidate only when no Profile row exists for
// its UUID. The existence check and the create commit in one ApplyBatch, so
// concurrent first logins of the same account cannot both succeed: the loser
// receives the winner's stored profile instead of minting a second,
// unmanaged ID alias for the same UUID.
func createProfileIfAbsent(db *store.Store, candidate *pb.Profile) (profile *pb.Profile, created bool, err error) {
	profileUUID, err := uuid.FromString(candidate.Uuid)
	if err != nil {
		return nil, false, err
	}
	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		existing := new(pb.Profile)
		getErr := Profile.Get(db, profileUUID.Bytes(), existing)
		if getErr == nil {
			// Mirror GetProfileFromUuid: a soft-deleted profile is not an
			// adoptable account, it is a rejected login.
			if existing.Deleted {
				return ErrProfileDeleted
			}
			profile = existing
			return nil
		}
		if !errors.Is(getErr, ErrNotFound) {
			return getErr
		}
		if err := stageProfile(db, batch, profileUUID, candidate); err != nil {
			return err
		}
		profile = candidate
		created = true
		return nil
	})
	return profile, created, err
}

// NewProfileFromOAuthUser writes a profile using the provider username as its
// ID, preserving the historical constructor semantics. Unlike the new
// get-or-create path, repeated calls with the same OAuth identity therefore
// reuse the same UserMap key instead of minting random aliases.
//
// Deprecated: new code must use GetOrCreateProfileFromOAuthUser, which is
// atomic per account, rejects soft-deleted profiles, and reports whether it
// created the profile.
func NewProfileFromOAuthUser(db *store.Store, authinfo *pb.OAuthUser) (*pb.Profile, error) {
	profile := &pb.Profile{
		Uuid:        authinfo.Uuid,
		Id:          authinfo.Name,
		Name:        authinfo.NickName,
		Type:        "user",
		Private:     false,
		Picture:     authinfo.AvatarUrl,
		Description: authinfo.Description,
	}
	if err := UpdateProfile(db, profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func UpdateProfile(db *store.Store, profile *pb.Profile) error {
	profileUUID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return err
	}

	// All checks and both writes commit as one atomic batch, serialized with
	// other ApplyBatch callers on this Store: concurrent creates of the same
	// ID cannot both pass the collision check, and a failed commit leaves no
	// orphan UserMap mapping pointing at a missing Profile.
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		return stageProfile(db, batch, profileUUID, profile)
	})
}

// stageProfile validates profile's ID against the rename reservation and the
// UserMap collision rules, then stages UserMap[id] -> uuid and the Profile
// row into batch. Callers must hold the Store's ApplyBatch critical section.
func stageProfile(db *store.Store, batch *pebble.Batch, profileUUID uuid.UUID, profile *pb.Profile) error {
	if reservedBy, err := FindProfileRenameByOldId(db, profile.Id); err == nil {
		return fmt.Errorf("%w: %q held by profile %s", ErrProfileIdReserved, profile.Id, reservedBy)
	} else if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("check profile ID %q against UserRenameMap: %w", profile.Id, err)
	}

	// user id(login) to uuid map. An ID already mapped to a different
	// profile must never be silently hijacked: renames clear the old
	// mapping through RenameProfileId before UpdateProfile runs, so a
	// conflicting mapping here is always an error.
	k := NewKeyFrom(TableUserMap.Bytes(), []byte(profile.Id))
	if existing, err := db.Get(k); err == nil && len(existing) > 0 {
		if mapped, err := uuid.FromBytes(existing); err != nil || mapped != profileUUID {
			return fmt.Errorf("%w: %q", ErrProfileIdTaken, profile.Id)
		}
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("read UserMap[%s]: %w", profile.Id, err)
	}
	if err := batch.Set(k, profileUUID.Bytes(), nil); err != nil {
		return err
	}

	// uuid map to user basic profile info
	return setProto(batch, Profile.PrefixAppend(profileUUID.Bytes()), profile)
}

func GetProfileFromUserId(db *store.Store, id string) (*pb.Profile, error) {
	k := NewKeyFrom(TableUserMap.Bytes(), []byte(id))
	rawdata, err := db.Get(k)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("GetProfile error: missing id->uuid map: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("GetProfile error: read id->uuid map: %w", err)
	}
	profileUUID, err := uuid.FromBytes(rawdata)
	if err != nil {
		return nil, err
	}
	return GetProfileFromUuid(db, profileUUID)
}

func GetProfileFromUuid(db *store.Store, profileUUID uuid.UUID) (*pb.Profile, error) {
	msg, err := GetStoredProfileFromUuid(db, profileUUID)
	if err != nil {
		return nil, err
	}
	if msg.Deleted {
		return nil, ErrProfileDeleted
	}
	return msg, nil
}

// GetStoredProfileFromUuid returns the persisted Profile row without mapping
// the Deleted flag to ErrProfileDeleted. It exists for bounded administrative
// inspection and state validation; ordinary product reads must continue to
// use GetProfileFromUuid so soft-deleted profiles remain unavailable.
func GetStoredProfileFromUuid(db *store.Store, profileUUID uuid.UUID) (*pb.Profile, error) {
	if profileUUID == uuid.Nil {
		return nil, errors.New("profile UUID is required")
	}
	msg := new(pb.Profile)
	if err := Profile.Get(db, profileUUID.Bytes(), msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// StageSetUserFeedPrivacy updates only the privacy bit of a live user Feed.
// Group privacy is immutable and system fields are preserved because the
// stored Profile is cloned rather than reconstructed from editable fields.
// Callers must invoke it from Store.ApplyBatch while holding the server's
// profile-update mutation lock.
func StageSetUserFeedPrivacy(db *store.Store, batch *pebble.Batch, profile *pb.Profile, private bool) (*pb.Profile, error) {
	if db == nil || batch == nil || profile == nil {
		return nil, errors.New("store, batch, and profile are required")
	}
	profileUUID, err := uuid.FromString(profile.Uuid)
	if err != nil || profileUUID == uuid.Nil {
		return nil, errors.New("profile UUID is invalid")
	}
	if profile.Deleted {
		return nil, ErrProfileDeleted
	}
	if profile.Type != "user" {
		return nil, fmt.Errorf("Feed privacy management only supports user feeds, got %q", profile.Type)
	}
	updated := proto.Clone(profile).(*pb.Profile)
	updated.Private = private
	if err := setProto(batch, Profile.PrefixAppend(profileUUID.Bytes()), updated); err != nil {
		return nil, err
	}
	// Public feeds should never carry requests, but clear the prefix on either
	// transition so stale requests from an earlier private period cannot become
	// actionable when an operator makes the feed private again.
	if err := StageDeleteFollowRequestsByTarget(db, batch, profileUUID); err != nil {
		return nil, err
	}
	return updated, nil
}

func ProfileToFeedinfo(profile *pb.Profile) *pb.Feedinfo {
	return &pb.Feedinfo{
		Uuid:        profile.Uuid,
		Id:          profile.Name,
		Name:        profile.Name,
		Type:        profile.Type,
		Private:     profile.Private,
		Picture:     profile.Picture,
		Description: profile.Description,
		Following:   []*pb.Profile{},
		Followers:   []*pb.Profile{},
		Admins:      []*pb.Profile{},
		Feeds:       []*pb.Profile{},
		Services:    []*pb.FeedService{},
	}
}

func ParseFollowerKey(k store.Key) store.Key {
	key := Follower.PrefixRemove(k)
	return key[uuid.Size:]
}
