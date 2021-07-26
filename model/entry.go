package model

import (
	"fmt"
	"time"

	"github.com/gofrs/uuid"
	pb "github.com/yinhm/friendfeed/proto"
	"github.com/yinhm/friendfeed/search"
	store "github.com/yinhm/friendfeed/storage"
)

func PutEntry(db *store.Store, entry *pb.Entry) (store.Key, error) {
	if entry.FeedUuid == "" {
		entry.FeedUuid = entry.ProfileUuid // backward comptabile
	}
	userUuid, err := uuid.FromString(entry.ProfileUuid)
	if err != nil {
		return nil, err
	}
	feedUuid, err := uuid.FromString(entry.FeedUuid)
	if err != nil {
		return nil, err
	}

	// unique key:
	// | table | entry uuid |

	// force full update
	entryUuid, err := uuid.FromString(entry.Id)
	if err != nil {
		return nil, err
	}
	key, err := Entry.Put(db, entryUuid.Bytes(), entry)
	if err != nil {
		return nil, err
	}

	// index entry key
	oldtime, err := time.Parse(time.RFC3339, entry.Date)
	if err != nil {
		return nil, err
	}
	err = EntryIndex.Index(db, userUuid, oldtime, key[:])
	if err != nil {
		return nil, err
	}
	if userUuid != feedUuid { // post to group
		err = EntryIndex.Index(db, feedUuid, oldtime, key[:])
		if err != nil {
			return nil, err
		}
	}

	// index entry body
	if entry.Body != "" {
		search.Indexer.Index(entry.Id, entry.Body)
	}

	return key, nil
}

func GetEntry(db *store.Store, uuidStr string) (*pb.Entry, error) {
	uuid1, err := uuid.FromString(uuidStr)
	if err != nil {
		return nil, err
	}

	entry := new(pb.Entry)
	err = Entry.Get(db, uuid1.Bytes(), entry)
	if err != nil {
		return nil, fmt.Errorf("entry not found: %s", uuidStr)
	}
	return entry, nil
}

func DeleteEntry(db *store.Store, uuidStr string) error {
	uuid1, err := uuid.FromString(uuidStr)
	if err != nil {
		return err
	}

	entry, err := GetEntry(db, uuidStr)
	if err != nil {
		return nil // blink delete
	}

	// delete entry from user feed
	// index entry key
	oldtime, err := time.Parse(time.RFC3339, entry.Date)
	if err != nil {
		return err
	}
	profileUuid, err := uuid.FromString(entry.ProfileUuid)
	if err != nil {
		return err
	}
	EntryIndex.RemoveIndex(db, profileUuid, oldtime)

	// delete group index aswell
	if entry.FeedUuid != entry.ProfileUuid && entry.FeedUuid != "" {
		feedUuid, err := uuid.FromString(entry.FeedUuid)
		if err != nil {
			return err
		}
		EntryIndex.RemoveIndex(db, feedUuid, oldtime)
	}

	if err = Entry.Delete(db, uuid1.Bytes()); err != nil {
		return err
	}

	// delete entry from public index??
	return nil
}
