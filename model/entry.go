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
	// unique key:
	// | table | entry uuid |

	// just force update
	uuidEntryKey := uuid.Must(uuid.FromString(entry.Id))
	key, err := Entry.Put(db, uuidEntryKey.Bytes(), entry)
	if err != nil {
		return nil, err
	}

	userUuid, err := uuid.FromString(entry.ProfileUuid)
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

	if err = Entry.Delete(db, uuid1.Bytes()); err != nil {
		return err
	}
	return nil
}
