package model

import (
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

// ErrProfileDeleted is returned by GetProfileFromUuid when the profile
// exists but is marked deleted. Callers should treat it like not-found.
var ErrProfileDeleted = errors.New("profile deleted")

// ErrProfileIdTaken is returned when a write would map a profile ID to a
// different profile than the one already holding it in UserMap.
var ErrProfileIdTaken = errors.New("profile ID is already taken")

// profileIdAlphabet is the character set for generated profile IDs. Together
// with the literal "ff-" prefix it keeps generated IDs inside the
// ValidateProfileId charset.
const profileIdAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// generatedProfileIdLength is the number of random characters after the
// "ff-" prefix. 36^8 combinations make collisions vanishingly rare;
// NewProfileFromOAuthUser still verifies availability before writing.
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

func NewProfileFromOAuthUser(db *store.Store, authinfo *pb.OAuthUser) (*pb.Profile, error) {
	// New profiles get a system-generated ID ("ff-" + random). The provider
	// display name is kept in Name only: it is not unique and often not a
	// valid feed slug. Users pick a meaningful ID later through the profile
	// page rename flow.
	for attempt := 0; attempt < 10; attempt++ {
		id, err := generateProfileId()
		if err != nil {
			return nil, err
		}
		profile := &pb.Profile{
			Uuid:        authinfo.Uuid,
			Id:          id,
			Name:        authinfo.NickName,
			Type:        "user",
			Private:     false,
			Picture:     authinfo.AvatarUrl,
			Description: authinfo.Description,
		}
		if err := UpdateProfile(db, profile); err != nil {
			if errors.Is(err, ErrProfileIdTaken) {
				continue
			}
			return nil, err
		}
		return profile, nil
	}
	return nil, errors.New("could not allocate a unique profile ID")
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
	k := NewKeyFrom(TableUserMap.Bytes(), []byte(profile.Id))
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		if reservedBy, err := FindProfileRenameByOldId(db, profile.Id); err == nil {
			return fmt.Errorf("profile ID %q is reserved by a previous rename of profile %s", profile.Id, reservedBy)
		} else if !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("check profile ID %q against UserRenameMap: %w", profile.Id, err)
		}

		// user id(login) to uuid map. An ID already mapped to a different
		// profile must never be silently hijacked: renames clear the old
		// mapping through RenameProfileId before UpdateProfile runs, so a
		// conflicting mapping here is always an error.
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
	})
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
	// key := NewUUIDKey(TableProfile, profileUUID)
	msg := new(pb.Profile)
	err := Profile.Get(db, profileUUID.Bytes(), msg)
	if err != nil {
		return nil, err
	}
	if msg.Deleted {
		return nil, ErrProfileDeleted
	}
	return msg, nil
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
