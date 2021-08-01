package model

import (
	"fmt"
	"strings"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	store "github.com/yinhm/friendfeed/storage"
)

// example id: "twitter:233666"
// Make sure combinated id never change
// for example twitter user change its username
func _oauthUserIdFrom(provider, userId string) store.Key {
	id := strings.ToLower(fmt.Sprintf("%s:%s", provider, userId))
	return []byte(id)
}

// return OAuth info for user like "twitter:233666"
func GetOAuthUser(db *store.Store, provider, userId string) (store.Key, *pb.OAuthUser, error) {
	internalUserId := _oauthUserIdFrom(provider, userId)

	msg := new(pb.OAuthUser)
	err := OAuth.Get(db, internalUserId, msg)
	if err != nil {
		return nil, nil, err
	}
	return internalUserId, msg, nil
}

// TODO: rename to RefreshOAuthUser?
// OAuth info can only change from upstream provider
func PutOAuthUser(db *store.Store, u *pb.OAuthUser) (*pb.OAuthUser, error) {
	_, v, err := GetOAuthUser(db, u.Provider, u.UserId)
	if err != nil && err != ErrNotFound {
		return nil, err
	}

	if v != nil {
		if u.Uuid != "" && v.Uuid != "" {
			uuid1, _ := uuid.FromString(u.Uuid)
			uuid2, _ := uuid.FromString(v.Uuid)
			if uuid1 != uuid2 {
				return nil, fmt.Errorf("user mismatch")
			}
		}
		if u.Uuid == "" {
			u.Uuid = v.Uuid
		}
	}

	// create/refresh OAuth User info
	internalUserId := _oauthUserIdFrom(u.Provider, u.UserId)
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
	// retrieve "Twitter:bob"
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
		return nil, fmt.Errorf("can not bind to another user.")
	}

	// first time bind
	msg.Uuid = u.Uuid

	_, err = OAuth.Put(db, key, msg)
	if err != nil {
		return nil, err
	}
	return msg, nil
}
