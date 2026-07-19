package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
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

	// fanout to feed followers(user timeline)
	FanoutEntry(db, userUuid, feedUuid, oldtime, key[:])

	// index entry body
	if entry.Body != "" {
		search.Indexer.Index(entry.Id, entry.Body)
	}

	return key, nil
}

func FanoutEntry(db *store.Store, userUuid, feedUuid uuid.UUID,
	oldtime time.Time, entryKey store.Key) (n int, err error) {
	fanOutToTimeline := UniqueKeyFrom(fmt.Sprintf("%x", userUuid), "user", "timeline")
	// fmt.Println(hex.EncodeToString(fanOutToTimeline.Bytes()))
	EntryIndex.Index(db, fanOutToTimeline, oldtime, entryKey)

	return updateFollowerTimelines(db, feedUuid, func(timelineUuid uuid.UUID) error {
		return EntryIndex.Index(db, timelineUuid, oldtime, entryKey)
	})
}

func GetEntry(db *store.Store, uuidStr string) (*pb.Entry, error) {
	uuid1, err := uuid.FromString(uuidStr)
	if err != nil {
		return nil, err
	}

	entry := new(pb.Entry)
	err = Entry.Get(db, uuid1.Bytes(), entry)
	if err != nil {
		return nil, fmt.Errorf("entry %s: %w", uuidStr, err)
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
		if errors.Is(err, ErrNotFound) {
			return nil // blind delete
		}
		return err
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

		DeleteFanoutEntry(db, profileUuid, feedUuid, oldtime)
	}

	if err = Entry.Delete(db, uuid1.Bytes()); err != nil {
		return err
	}

	// delete entry from public index??
	return nil
}

func DeleteFanoutEntry(db *store.Store, userUuid, feedUuid uuid.UUID,
	oldtime time.Time) (n int, err error) {
	fanOutToTimeline := UniqueKeyFrom(fmt.Sprintf("%x", userUuid), "user", "timeline")
	EntryIndex.RemoveIndex(db, fanOutToTimeline, oldtime)

	return updateFollowerTimelines(db, feedUuid, func(timelineUuid uuid.UUID) error {
		return EntryIndex.RemoveIndex(db, timelineUuid, oldtime)
	})
}

func updateFollowerTimelines(db *store.Store, feedUuid uuid.UUID, update func(uuid.UUID) error) (n int, err error) {
	prefix := NewPrefixKeyFrom(TableFollower, feedUuid.Bytes())
	// fmt.Printf("scan key, %s\n", prefix.String())
	return db.ForwardScan(prefix, func(i int, k, v []byte) error {
		fk := ParseFollowerKey(k)
		timelineUuid := UniqueKeyFrom(fk.String(), "user", "timeline")
		// fmt.Printf("fanout to: <%s, %x>", fk.String(), timelineUuid)
		return update(timelineUuid)
	})
}

func PutTweet(db *store.Store, tweet *pb.Tweet) error {
	_, err := Tweet.Put(db, []byte(tweet.Id), tweet)
	return err
}
