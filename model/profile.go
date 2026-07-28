package model

import (
	"errors"
	"fmt"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

// ErrProfileDeleted is returned by GetProfileFromUuid when the profile
// exists but is marked deleted. Callers should treat it like not-found.
var ErrProfileDeleted = errors.New("profile deleted")

func NewProfileFromOAuthUser(db *store.Store, authinfo *pb.OAuthUser) (*pb.Profile, error) {
	// create new profile on oauth
	profile := &pb.Profile{
		Uuid:        authinfo.Uuid,
		Id:          authinfo.Name, // userid may better, but use name anyway
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
	if reservedBy, err := FindProfileRenameByOldId(db, profile.Id); err == nil {
		return fmt.Errorf("profile ID %q is reserved by a previous rename of profile %s", profile.Id, reservedBy)
	} else if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("check profile ID %q against UserRenameMap: %w", profile.Id, err)
	}

	// user id(login) to uuid map
	k := NewKeyFrom(TableUserMap.Bytes(), []byte(profile.Id))
	if err := db.Put(k, profileUUID.Bytes()); err != nil {
		return err
	}
	// log.Println("id->uuid map updated", profile.Id, "->", profile.Uuid)

	// uuid map to user basic profile info
	_, err = Profile.Put(db, profileUUID.Bytes(), profile)
	return err
}

func GetProfileFromUserId(db *store.Store, id string) (*pb.Profile, error) {
	k := NewKeyFrom(TableUserMap.Bytes(), []byte(id))
	rawdata, err := db.Get(k)
	if err != nil || string(rawdata) == "" {
		return nil, errors.New("GetProfile error: missing id->uuid map")
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
		Services:    []*pb.Service{},
	}
}

func ParseFollowerKey(k store.Key) store.Key {
	key := Follower.PrefixRemove(k)
	return key[uuid.Size:]
}
