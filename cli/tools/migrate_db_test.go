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
	// Legacy OAuth rows can be bound by login name without carrying UUID.
	if _, err := model.OAuth.Put(db, model.KeyFromString("twitter", "active-user"), &pb.OAuthUser{
		Name: "user", Provider: "twitter", UserId: "active-user",
	}); err != nil {
		t.Fatal(err)
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
	if stats.profiles != 1 || stats.follows != 1 || stats.entries != 2 {
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

func TestExplicitTimelineUserDoesNotRequireOAuthMetadata(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	userID := uuid.Must(uuid.NewV4())
	if err := model.UpdateProfile(db, &pb.Profile{Uuid: userID.String(), Id: "yinhm", Type: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := model.EntryIndex.Index(db, userID, time.Unix(100, 0), []byte("own-entry")); err != nil {
		t.Fatal(err)
	}

	stats, err := rebuildTimelines(db, timelineRebuildOptions{user: "yinhm", maxFeeds: 20, dryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.profiles != 1 || stats.entries != 1 {
		t.Fatalf("unexpected explicit-user stats: %+v", stats)
	}
}

func TestRebuildSocialGraphFromLegacyFeedinfo(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	followerID := uuid.Must(uuid.NewV4())
	feedID := uuid.Must(uuid.NewV4())
	follower := &pb.Profile{Uuid: followerID.String(), Id: "follower", Type: "user"}
	feed := &pb.Profile{Uuid: feedID.String(), Id: "feed", Type: "user"}
	for _, profile := range []*pb.Profile{follower, feed} {
		if err := model.UpdateProfile(db, profile); err != nil {
			t.Fatal(err)
		}
	}

	// The current field names intentionally hold legacy wire semantics:
	// Feedinfo.Followers (field 11) was subscriptions.
	if err := model.PutFeedinfo(db, follower.Uuid, &pb.Feedinfo{
		Uuid: follower.Uuid, Id: follower.Id, Followers: []*pb.Profile{feed},
	}); err != nil {
		t.Fatal(err)
	}
	// Feedinfo.Following (field 10) was subscribers. This is reciprocal
	// evidence for the same edge and must be deduplicated.
	if err := model.PutFeedinfo(db, feed.Uuid, &pb.Feedinfo{
		Uuid: feed.Uuid, Id: feed.Id, Following: []*pb.Profile{follower},
	}); err != nil {
		t.Fatal(err)
	}

	dryStats, err := rebuildSocialGraph(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if dryStats.edges != 1 || db.Exist(model.NewKeyFrom(model.Follow.Prefix, followerID.Bytes(), feedID.Bytes())) {
		t.Fatalf("unexpected social graph dry run: %+v", dryStats)
	}

	stats, err := rebuildSocialGraph(db, false)
	if err != nil {
		t.Fatal(err)
	}
	if stats.feedinfos != 2 || stats.edges != 1 || stats.skipped != 0 {
		t.Fatalf("unexpected social graph stats: %+v", stats)
	}
	followKey := model.NewKeyFrom(model.Follow.Prefix, followerID.Bytes(), feedID.Bytes())
	followerKey := model.NewKeyFrom(model.Follower.Prefix, feedID.Bytes(), followerID.Bytes())
	if !db.Exist(followKey) || !db.Exist(followerKey) {
		t.Fatal("rebuild did not create both Follow and Follower keys")
	}
}
