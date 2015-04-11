package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang/protobuf/proto"
	uuid "github.com/satori/go.uuid"
	pb "github.com/yinhm/friendfeed/proto"
)

// TODO: refactor, introduce another interface above proto.Message?

type ScanCallback func(int, []byte, []byte) error

type Error struct {
	Msg  string
	Code ErrorCode
}

func (e *Error) Error() string { return e.Msg }

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
	if !update {
		// query if it exists?
		value, err := rdb.Get(kb1)
		if err == nil && value != nil { // already exists
			// log.Println("key exists, skip update:", key.String())
			return key, &Error{"ok", ExistItem}
		}
	}
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
	if profile.RemoteKey != "" {
		return mdb.Put(key.Bytes(), bytes)
	}

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
		profile.RemoteKey = old.RemoteKey
	}

	bytes, err = proto.Marshal(profile)
	if err != nil {
		return err
	}
	return mdb.Put(key.Bytes(), bytes)
}

func GetProfile(mdb *Store, id string) (*pb.Profile, error) {
	data, err := mdb.Get([]byte(id))
	if err != nil || string(data) == "" {
		return nil, fmt.Errorf("GetProfile error: missing id->uuid map")
	}

	uuid1, err := uuid.FromBytes(data)
	if err != nil {
		return nil, err
	}

	key := NewUUIDKey(TableProfile, uuid1)
	data, err = mdb.Get(key.Bytes())
	if err != nil {
		return nil, err
	}
	v := new(pb.Profile)
	err = proto.Unmarshal(data, v)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func GetEntry(rdb *Store, uuidStr string) (*pb.Entry, error) {
	uuid1, err := uuid.FromString(uuidStr)
	if err != nil {
		return nil, err
	}

	key := NewUUIDKey(TableEntry, uuid1)
	rawdata, err := rdb.Get(key.Bytes())
	if err != nil {
		return nil, err
	}

	entry := new(pb.Entry)
	err = proto.Unmarshal(rawdata, entry)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// id: target id or user id, eg: foobar
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
	if info.RemoteKey != "" {
		return rdb.Put(key.Bytes(), bytes)
	}

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
		info.RemoteKey = old.RemoteKey
	}

	bytes, err = proto.Marshal(info)
	if err != nil {
		return err
	}

	return rdb.Put(key.Bytes(), bytes)
}

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

func ForwardTableScan(db *Store, prefix Key, fn ScanCallback) (n int, err error) {
	iter := db.Iterator()
	defer iter.Close()

	iter.Seek(prefix.Bytes())
	for ; iter.ValidForPrefix(prefix.Bytes()); iter.Next() {
		kSlice := iter.Key()
		vSlice := iter.Value()
		defer kSlice.Free()
		defer vSlice.Free()

		if err = fn(n, kSlice.Data(), vSlice.Data()); err != nil {
			if serr, ok := err.(*Error); ok {
				if serr.Code == StopIteration {
					return n, nil // rewrote err
				}
			}
			return
		}
		n++
	}
	return
}

func UpdateOAuthUser(mdb *Store, u *pb.OAuthUser) (*pb.OAuthUser, error) {
	var pt PrefixTable
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
	if len(rawdata) != 0 {
		v := new(pb.OAuthUser)
		err = proto.Unmarshal(rawdata, v)
		if err != nil {
			return nil, err
		}
		u.Uuid = v.Uuid
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

func BindOAuthUser(mdb *Store, u *pb.OAuthUser) (*pb.OAuthUser, error) {
	var pt PrefixTable
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
