package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/golang/protobuf/proto"
	pb "github.com/yinhm/friendfeed/proto"
)

const (
	TableFeed     KeyPrefix = 1
	TableFeedinfo KeyPrefix = 2
	TableEntry    KeyPrefix = 3

	// TODO: obsoleted TableEntryIndex, FixMaxEntryIndex
	// WARN: TableEntryIndex > TableEntry for FixMaxEntryIndex
	TableEntryIndex KeyPrefix = 4
	// TableEntryIndex NOT working, BackwardFetchFeed broken
	// duplicate a reverse index
	TableReverseEntryIndex KeyPrefix = 5
	TableIndexCache        KeyPrefix = 6

	TableProfile      KeyPrefix = 100
	TableService      KeyPrefix = 101
	TableSubscription KeyPrefix = 102
	TableSubscriber   KeyPrefix = 103
	TableOAuthTwitter KeyPrefix = 104
	TableOAuthGoogle  KeyPrefix = 105

	TableJobFeed    KeyPrefix = 200
	TableJobRunning KeyPrefix = 201
	TableJobHistory KeyPrefix = 202

	TableMax KeyPrefix = 1e8
)

// Deprecated: use model.PutEntry instead
func PutEntry(rdb *Store, entry *pb.Entry, update bool) (*UUIDKey, error) {
	uuid1, err := uuid.FromString(entry.ProfileUuid)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(entry.Id, "e/") {
		entry.Id = strings.TrimPrefix(entry.Id, "e/")
	}

	// marshal should after uuid trimmed
	bytes, err := proto.Marshal(entry)
	if err != nil {
		return nil, err
	}

	uuid2, err := uuid.FromString(entry.Id)
	if err != nil {
		return nil, err
	}

	// static unique key:
	// | table | entry uuid |
	key := NewUUIDKey(TableEntry, uuid2)
	kb1 := key.Bytes()
	// is entry exists?
	value, err := rdb.Get(kb1)
	if err == nil && value != nil { // already exists
		// TODO: it is not safe to update(comments/likes)
		// split comments/likes from entry
		if update {
			if err := rdb.Put(kb1, bytes); err != nil {
				return nil, err
			}
			return key, nil
		}
		return key, &Error{"ok", ExistItem}
	}

	// not exists
	if err := rdb.Put(kb1, bytes); err != nil {
		return nil, err
	}

	// Entry index list:
	// K-> | table | user uuid | snowflake |
	// V-> |  +++++   entry key   ++++++   |
	oldtime, err := time.Parse(time.RFC3339, entry.Date)
	if err != nil {
		return nil, err
	}
	// flakeid := rdb.TimeTravelId(oldtime)
	// key2 := NewUUIDFlakeKey(TableEntryIndex, uuid1, flakeid)
	// err = rdb.Put(key2.Bytes(), kb1)
	// if err != nil {
	// 	return nil, err
	// }

	// Reverse Entry index:
	// K-> | table | user uuid | max-minus-ts-flake |
	// V-> |       +++++   entry key   ++++++       |
	flakeid := rdb.TimeTravelReverseId(oldtime)
	key3 := NewUUIDFlakeKey(TableReverseEntryIndex, uuid1, flakeid)
	err = rdb.Put(key3.Bytes(), kb1)
	if err != nil {
		return nil, err
	}

	return key, nil
}

// Deprecated: use model.UpdateProfile instead
func UpdateProfile(mdb *Store, profile *pb.Profile) error {
	bytes, err := proto.Marshal(profile)
	if err != nil {
		return err
	}

	uuid1, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return err
	}

	// user id(login) to uuid map
	if err := mdb.Put([]byte(profile.Id), uuid1[:]); err != nil {
		return err
	}
	// log.Println("id->uuid map updated", profile.Id, "->", profile.Uuid)

	// uuid map to user basic profile info
	key := NewUUIDKey(TableProfile, uuid1)
	// if profile.RemoteKey != "" {
	// 	return mdb.Put(key.Bytes(), bytes)
	// }

	// retrieve remote key
	rawdata, err := mdb.Get(key.Bytes())
	if err != nil {
		return err
	}

	if len(rawdata) != 0 {
		old := new(pb.Profile)
		err = proto.Unmarshal(rawdata, old)
		if err != nil {
			return err
		}
	}

	bytes, err = proto.Marshal(profile)
	if err != nil {
		return err
	}
	return mdb.Put(key.Bytes(), bytes)
}

// Deprecated: use model.GetProfileFromUserId instead
func GetProfileFromUserId(mdb *Store, id string) (*pb.Profile, error) {
	rawdata, err := mdb.Get([]byte(id))
	if err != nil || string(rawdata) == "" {
		return nil, fmt.Errorf("GetProfile error: missing id->uuid map")
	}
	uuid1, err := uuid.FromBytes(rawdata)
	if err != nil {
		return nil, err
	}
	return GetProfileFromUuid(mdb, uuid1)
}

// Deprecated: use model.GetProfileFromUuid instead
func GetProfileFromUuid(mdb *Store, uuid1 uuid.UUID) (*pb.Profile, error) {
	key := NewUUIDKey(TableProfile, uuid1)
	rawdata, err := mdb.Get(key.Bytes())
	if err != nil {
		return nil, err
	}
	v := new(pb.Profile)
	err = proto.Unmarshal(rawdata, v)
	if err != nil {
		return nil, err
	}
	if v.Deleted {
		return nil, fmt.Errorf("Profile deleted")
	}
	return v, nil
}

// Deprecated: use model.PutEntry instead
func GetEntry(rdb *Store, uuidStr string) (*pb.Entry, error) {
	uuid1, err := uuid.FromString(uuidStr)
	if err != nil {
		return nil, err
	}

	key := NewUUIDKey(TableEntry, uuid1)
	rawdata, err := rdb.Get(key.Bytes())
	if err != nil || len(rawdata) == 0 {
		return nil, fmt.Errorf("entry not found: %s", key.String())
	}

	entry := new(pb.Entry)
	err = proto.Unmarshal(rawdata, entry)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// Deprecated: use model.PutEntry instead
func DeleteEntry(rdb *Store, uuidStr string) error {
	uuid1, err := uuid.FromString(uuidStr)
	if err != nil {
		return err
	}

	key := NewUUIDKey(TableEntry, uuid1)
	if err = rdb.Delete(key.Bytes()); err != nil {
		return fmt.Errorf("entry not found: %s", key.String())
	}
	return nil
}

// id: target id or user id, eg: foobar
// Obsoleted
func GetArchiveHistory(mdb *Store, id string) (*pb.FeedJob, error) {
	key := NewMetaKey(TableJobHistory, id)
	rawdata, err := mdb.Get(key.Bytes())
	if err != nil {
		return nil, err
	}

	job := new(pb.FeedJob)
	err = proto.Unmarshal(rawdata, job)
	if err != nil {
		return nil, err
	}
	return job, nil
}

// uuid -> services
// func SaveFeedServices(rdb *Store, uuidStr string, services []*pb.Service) error {
// 	uuid1, err := uuid.FromString(uuidStr)
// 	if err != nil {
// 		return err
// 	}
//  // TODO: proto marshal slice?
// 	bytes, err := proto.Marshal(services)
// 	if err != nil {
// 		return err
// 	}

// 	key := NewUUIDKey(TableService, uuid1)
// 	if err := t.db.Put(key.Bytes(), bytes); err != nil {
// 		return err
// 	}
// 	return nil
// }

// uuid -> feedinfo
// Deprecated: use model.SaveFeedinfo
func SaveFeedinfo(rdb *Store, uuidStr string, info *pb.Feedinfo) error {
	uuid1, err := uuid.FromString(uuidStr)
	if err != nil {
		return err
	}

	bytes, err := proto.Marshal(info)
	if err != nil {
		return err
	}

	key := NewUUIDKey(TableFeedinfo, uuid1)
	// if info.RemoteKey != "" {
	// 	return rdb.Put(key.Bytes(), bytes)
	// }

	// retrieve remote key
	rawdata, err := rdb.Get(key.Bytes())
	if err != nil {
		return err
	}

	if len(rawdata) != 0 {
		old := new(pb.Feedinfo)
		err = proto.Unmarshal(rawdata, old)
		if err != nil {
			return err
		}
	}

	bytes, err = proto.Marshal(info)
	if err != nil {
		return err
	}

	return rdb.Put(key.Bytes(), bytes)
}

// TODO: move feedinfo to mdb?
// Deprecated: use model.GetFeedinfo
func GetFeedinfo(rdb *Store, uuidStr string) (*pb.Feedinfo, error) {
	uuid1, err := uuid.FromString(uuidStr)
	if err != nil {
		return nil, err
	}

	key := NewUUIDKey(TableFeedinfo, uuid1)
	rawdata, err := rdb.Get(key.Bytes())
	if err != nil {
		return nil, err
	}

	info := new(pb.Feedinfo)
	err = proto.Unmarshal(rawdata, info)
	if err != nil {
		return nil, err
	}
	return info, nil
}

// Deprecated: use model.GetOAuthUser
func GetOAuthUser(mdb *Store, provider, userId string) (IKey, *pb.OAuthUser, error) {
	var pt KeyPrefix
	switch provider {
	case "google":
		pt = TableOAuthGoogle
	case "twitter":
		pt = TableOAuthTwitter
	}

	key := NewMetaKey(pt, userId)
	rawdata, err := mdb.Get(key.Bytes())
	if err != nil {
		return nil, nil, err
	}
	if len(rawdata) == 0 {
		return key, nil, nil
	}

	v := new(pb.OAuthUser)
	err = proto.Unmarshal(rawdata, v)
	if err != nil {
		return nil, nil, err
	}
	return key, v, nil
}

// Deprecated: use model.PutOAuthUser
func PutOAuthUser(mdb *Store, u *pb.OAuthUser) (*pb.OAuthUser, error) {
	key, v, err := GetOAuthUser(mdb, u.Provider, u.UserId)
	if err != nil {
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

	bytes, err := proto.Marshal(u)
	if err != nil {
		return nil, err
	}

	// refresh OAuth User info to store
	err = mdb.Put(key.Bytes(), bytes)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// Deprecated: use model.BindOAuthUser
func BindOAuthUser(mdb *Store, u *pb.OAuthUser) (*pb.OAuthUser, error) {
	var pt KeyPrefix
	switch u.Provider {
	case "google":
		pt = TableOAuthGoogle
	case "twitter":
		pt = TableOAuthTwitter
	}

	key := NewMetaKey(pt, u.UserId)
	rawdata, err := mdb.Get(key.Bytes())
	if err != nil {
		return nil, err
	}

	// retrieve uuid
	if len(rawdata) == 0 {
		return nil, fmt.Errorf("No user data")
	}

	v := new(pb.OAuthUser)
	err = proto.Unmarshal(rawdata, v)
	if err != nil {
		return nil, err
	}
	// same bind
	if u.Uuid == v.Uuid {
		return v, nil
	}

	if v.Uuid != "" {
		return nil, fmt.Errorf("can not bind to another user.")
	}

	// first time bind
	v.Uuid = u.Uuid
	bytes, err := proto.Marshal(u)
	if err != nil {
		return nil, err
	}

	err = mdb.Put(key.Bytes(), bytes)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// Deprecated:
func Like(rdb *Store, profile *pb.Profile, entry *pb.Entry) (*UUIDKey, *pb.Entry, error) {
	var err error
	var key *UUIDKey
	index := -1
	for i, like := range entry.Likes {
		if like.From.Id == profile.Id {
			index = i
			break
		}
	}
	if index == -1 {
		like := &pb.Like{
			Date: time.Now().Format(time.RFC3339),
			From: &pb.Feed{
				Id:   profile.Id,
				Name: profile.Name,
				Type: profile.Type,
			},
		}
		entry.Likes = append(entry.Likes, like)
		key, err = PutEntry(rdb, entry, true)
	}
	return key, entry, err
}

// Deprecated:
func DeleteLike(rdb *Store, profile *pb.Profile, entry *pb.Entry) (*pb.Entry, error) {
	var err error
	index := -1
	for i, like := range entry.Likes {
		if like.From.Id == profile.Id {
			index = i
			break
		}
	}
	if index >= 0 {
		entry.Likes = append(entry.Likes[:index], entry.Likes[index+1:]...)
		_, err = PutEntry(rdb, entry, true)
	}
	return entry, err
}

// Deprecated:
func Comment(rdb *Store, profile *pb.Profile, entry *pb.Entry, comment *pb.Comment) (*UUIDKey, *pb.Entry, error) {
	var err error

	// is update?
	idx := -1
	for i, cmt := range entry.Comments {
		if cmt.Id == comment.Id {
			// recheck perm
			if cmt.From.Id != comment.From.Id {
				return nil, nil, fmt.Errorf("403: perm error")
			}
			idx = i
			break
		}
	}
	if idx >= 0 {
		entry.Comments[idx] = comment
	} else {
		entry.Comments = append(entry.Comments, comment)
	}
	key, err := PutEntry(rdb, entry, true)
	return key, entry, err
}

// Deprecated:
func DeleteComment(rdb *Store, profile *pb.Profile, entry *pb.Entry, commentId string) (*pb.Entry, error) {
	var err error
	index := -1
	for i, cmt := range entry.Comments {
		if commentId == cmt.Id {
			index = i
			break
		}
	}
	if index >= 0 {
		entry.Comments = append(entry.Comments[:index], entry.Comments[index+1:]...)
		_, err = PutEntry(rdb, entry, true)
	}
	return entry, err
}
