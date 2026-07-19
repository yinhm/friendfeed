package model

import (
	"errors"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

// example id: "twitter:233666"
// Make sure combinated id never change
// for example twitter user change its username
func oauthUserIDFrom(provider, userId string) store.Key {
	return KeyFromString(provider, userId)
}

// return OAuth info for user like "twitter:233666"
func GetOAuthUser(db *store.Store, provider, userId string) (store.Key, *pb.OAuthUser, error) {
	internalUserId := oauthUserIDFrom(provider, userId)

	msg := new(pb.OAuthUser)
	err := OAuth.Get(db, internalUserId, msg)
	if err != nil {
		return nil, nil, err
	}
	return internalUserId, msg, nil
}

// TODO: rename to RefreshOAuthUser?
// OAuth info updated when login from upstream provider
func PutOAuthUser(db *store.Store, u *pb.OAuthUser) (*pb.OAuthUser, error) {
	_, v, err := GetOAuthUser(db, u.Provider, u.UserId)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	if v != nil {
		if u.Uuid != "" && v.Uuid != "" {
			uuid1, _ := uuid.FromString(u.Uuid)
			uuid2, _ := uuid.FromString(v.Uuid)
			if uuid1 != uuid2 {
				return nil, errors.New("user mismatch")
			}
		}
		if u.Uuid == "" {
			u.Uuid = v.Uuid
		}
	}

	// New user, uuid are the same for OauthUser/Profile/Feed/Feedinfo
	if u.Uuid == "" {
		uuid1 := UniqueKeyFrom(u.Provider, u.UserId)
		// u.Uuid = fmt.Sprintf("%x", uuid1)
		u.Uuid = uuid1.String()
	}

	// create/refresh OAuth User info
	internalUserId := oauthUserIDFrom(u.Provider, u.UserId)
	_, err = OAuth.Put(db, internalUserId, u)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// bind uuid to exists oauth user
// Deprecated: only used in FriendFeedImportHandler
// Obsoleted
func BindOAuthUser(db *store.Store, u *pb.OAuthUser) (*pb.OAuthUser, error) {
	// retrieve "Twitter:12345"
	key, msg, err := GetOAuthUser(db, u.Provider, u.UserId)
	if err != nil {
		return nil, err
	}

	// same bind
	if u.Uuid == msg.Uuid {
		return msg, nil
	}

	// not the same user?
	if msg.Uuid != "" {
		return nil, errors.New("can not bind to another user.")
	}

	// first time bind
	msg.Uuid = u.Uuid

	_, err = OAuth.Put(db, key, msg)
	if err != nil {
		return nil, err
	}
	return msg, nil
}
