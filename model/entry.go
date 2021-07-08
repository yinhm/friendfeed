package model

import (
	"time"

	"github.com/gofrs/uuid"
	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
)

func PutEntry(db *store.Store, entry *pb.Entry) (store.Key, error) {
	// unique key:
	// | table | entry uuid |

	// just force update
	key, err := Entry.Put(db, entry.Id, entry)
	if err != nil {
		return nil, err
	}

	userUuid, err := uuid.FromString(entry.ProfileUuid)
	if err != nil {
		return nil, err
	}

	// index entry
	oldtime, err := time.Parse(time.RFC3339, entry.Date)
	if err != nil {
		return nil, err
	}
	err = ReverseEntryIndex.Index(db, userUuid, oldtime, key)
	if err != nil {
		return nil, err
	}

	return key, nil
}
