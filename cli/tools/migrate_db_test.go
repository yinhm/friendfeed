package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestRebuildTimelines(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()
	db.SetSync(false)

	userID := uuid.Must(uuid.NewV4())
	followedID := uuid.Must(uuid.NewV4())
	for id, name := range map[uuid.UUID]string{userID: "user", followedID: "followed"} {
		if err := model.UpdateProfile(db, &pb.Profile{Uuid: id.String(), Id: name, Type: "user"}); err != nil {
			t.Fatal(err)
		}
	}

	followKey := model.NewKeyFrom(model.Follow.Prefix, userID.Bytes(), followedID.Bytes())
	if err := db.Set(followKey, []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := model.EntryIndex.Index(db, userID, time.Unix(100, 0), []byte("own-entry")); err != nil {
		t.Fatal(err)
	}
	if err := model.EntryIndex.Index(db, followedID, time.Unix(200, 0), []byte("followed-entry")); err != nil {
		t.Fatal(err)
	}
	timelineID := model.UniqueKeyFrom(fmt.Sprintf("%x", userID), "user", "timeline")
	if err := model.EntryIndex.Index(db, timelineID, time.Unix(300, 0), []byte("stale-entry")); err != nil {
		t.Fatal(err)
	}

	dryStats, err := rebuildTimelines(db, timelineRebuildOptions{user: "user", maxFeeds: 1, dryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dryStats.profiles != 1 || dryStats.existing != 1 || dryStats.follows != 0 || dryStats.entries != 1 {
		t.Fatalf("unexpected dry-run stats: %+v", dryStats)
	}
	stalePrefix := model.NewUUIDKey(model.TableEntryIndex, timelineID)
	staleCount, err := db.ForwardScan(stalePrefix, func(i int, key, value []byte) error { return nil })
	if err != nil || staleCount != 1 {
		t.Fatalf("dry-run modified timeline: count=%d, err=%v", staleCount, err)
	}

	stats, err := rebuildTimelines(db, timelineRebuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.profiles != 2 || stats.follows != 1 || stats.entries != 3 {
		t.Fatalf("unexpected rebuild stats: %+v", stats)
	}

	timelinePrefix := model.NewUUIDKey(model.TableEntryIndex, timelineID)
	var values []string
	if _, err := db.ForwardScan(timelinePrefix, func(i int, key, value []byte) error {
		values = append(values, string(value))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0] != "followed-entry" || values[1] != "own-entry" {
		t.Fatalf("unexpected rebuilt timeline: %v", values)
	}
}
