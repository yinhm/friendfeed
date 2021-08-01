package model

import (
	"fmt"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

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

// FIXME: id=>uuid map may collision on different sevices
func UpdateProfile(db *store.Store, profile *pb.Profile) error {
	uuid1, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return err
	}

	// user id(login) to uuid map
	if err := db.Put([]byte(profile.Id), uuid1.Bytes()); err != nil {
		return err
	}
	// log.Println("id->uuid map updated", profile.Id, "->", profile.Uuid)

	// uuid map to user basic profile info
	_, err = Profile.Put(db, uuid1.Bytes(), profile)
	return err
}

func GetProfileFromUserId(db *store.Store, id string) (*pb.Profile, error) {
	rawdata, err := db.Get([]byte(id))
	if err != nil || string(rawdata) == "" {
		return nil, fmt.Errorf("GetProfile error: missing id->uuid map")
	}
	uuid1, err := uuid.FromBytes(rawdata)
	if err != nil {
		return nil, err
	}
	return GetProfileFromUuid(db, uuid1)
}

func GetProfileFromUuid(db *store.Store, uuid1 uuid.UUID) (*pb.Profile, error) {
	// key := NewUUIDKey(TableProfile, uuid1)
	msg := new(pb.Profile)
	err := Profile.Get(db, uuid1.Bytes(), msg)
	if err != nil {
		return nil, err
	}
	if msg.Deleted {
		return nil, fmt.Errorf("Profile deleted")
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
