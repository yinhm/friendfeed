package main

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func seedActorProfile(t *testing.T, db *store.Store, id string) uuid.UUID {
	t.Helper()
	profileUUID := uuid.Must(uuid.NewV4())
	if err := model.UpdateProfile(db, &pb.Profile{
		Uuid: profileUUID.String(),
		Id:   id,
		Name: id,
		Type: "user",
	}); err != nil {
		t.Fatalf("seed profile %q: %v", id, err)
	}
	return profileUUID
}

func seedActorEntry(t *testing.T, db *store.Store, entry *pb.Entry) uuid.UUID {
	t.Helper()
	entryUUID := uuid.Must(uuid.NewV4())
	entry.Id = entryUUID.String()
	if _, err := model.Entry.Put(db, entryUUID.Bytes(), entry); err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	return entryUUID
}

func TestBackfillActorUUIDsDryRunApplyAndIdempotence(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	authorUUID := seedActorProfile(t, db, "author")
	commenterUUID := seedActorProfile(t, db, "commenter")
	likerUUID := seedActorProfile(t, db, "liker")
	entryUUID := seedActorEntry(t, db, &pb.Entry{
		From:     &pb.Feed{Id: "author", Name: "Old Author Snapshot"},
		Comments: []*pb.Comment{{From: &pb.Feed{Id: "commenter"}}},
		Likes:    []*pb.Like{{From: &pb.Feed{Id: "liker"}}},
	})

	dryStats, err := backfillActorUUIDs(db, actorUUIDBackfillOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if dryStats.entriesScanned != 1 || dryStats.entriesChanged != 1 ||
		dryStats.entryAuthors != 1 || dryStats.comments != 1 || dryStats.likes != 1 {
		t.Fatalf("unexpected dry-run stats: %+v", dryStats)
	}
	unchanged, err := model.GetEntry(db, entryUUID.String())
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.ProfileUuid != "" || unchanged.From.Uuid != "" ||
		unchanged.Comments[0].From.Uuid != "" || unchanged.Likes[0].From.Uuid != "" {
		t.Fatalf("dry-run mutated entry: %+v", unchanged)
	}

	stats, err := backfillActorUUIDs(db, actorUUIDBackfillOptions{apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats != dryStats {
		t.Fatalf("apply stats %+v differ from dry-run %+v", stats, dryStats)
	}
	migrated, err := model.GetEntry(db, entryUUID.String())
	if err != nil {
		t.Fatal(err)
	}
	if migrated.ProfileUuid != authorUUID.String() || migrated.From.Uuid != authorUUID.String() {
		t.Fatalf("entry author UUIDs = (%q, %q); want %q",
			migrated.ProfileUuid, migrated.From.Uuid, authorUUID)
	}
	if migrated.Comments[0].From.Uuid != commenterUUID.String() {
		t.Fatalf("comment UUID = %q; want %q", migrated.Comments[0].From.Uuid, commenterUUID)
	}
	if migrated.Likes[0].From.Uuid != likerUUID.String() {
		t.Fatalf("like UUID = %q; want %q", migrated.Likes[0].From.Uuid, likerUUID)
	}
	if migrated.From.Name != "Old Author Snapshot" {
		t.Fatalf("display snapshot changed to %q", migrated.From.Name)
	}

	repeated, err := backfillActorUUIDs(db, actorUUIDBackfillOptions{apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.entriesChanged != 0 || repeated.alreadySet != 3 {
		t.Fatalf("migration is not idempotent: %+v", repeated)
	}
}

func TestBackfillActorUUIDsPreservesConflictsAndSkipsUnresolved(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	authorUUID := seedActorProfile(t, db, "author")
	otherUUID := seedActorProfile(t, db, "other")
	entryUUID := seedActorEntry(t, db, &pb.Entry{
		ProfileUuid: otherUUID.String(),
		From:        &pb.Feed{Id: "author"},
		Comments: []*pb.Comment{
			{From: &pb.Feed{Id: "author", Uuid: otherUUID.String()}},
			{From: &pb.Feed{Id: "missing"}},
			nil,
		},
		Likes: []*pb.Like{
			{From: &pb.Feed{Id: "author", Uuid: "not-a-uuid"}},
			{},
		},
	})

	stats, err := backfillActorUUIDs(db, actorUUIDBackfillOptions{apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.entriesChanged != 0 || stats.conflicts != 3 || stats.unresolved != 3 {
		t.Fatalf("unexpected conflict stats: %+v", stats)
	}
	got, err := model.GetEntry(db, entryUUID.String())
	if err != nil {
		t.Fatal(err)
	}
	if got.ProfileUuid != otherUUID.String() || got.From.Uuid != "" {
		t.Fatalf("conflicting entry author was overwritten: %+v", got.From)
	}
	if got.Comments[0].From.Uuid != otherUUID.String() ||
		got.Likes[0].From.Uuid != "not-a-uuid" {
		t.Fatal("conflicting actor UUID was overwritten")
	}
	if authorUUID == otherUUID {
		t.Fatal("test setup generated duplicate UUIDs")
	}
}

func TestBackfillActorUUIDsFiltersByUserAndLimit(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	seedActorProfile(t, db, "wanted")
	seedActorProfile(t, db, "other")
	seedActorEntry(t, db, &pb.Entry{From: &pb.Feed{Id: "other"}})
	seedActorEntry(t, db, &pb.Entry{From: &pb.Feed{Id: "wanted"}})
	seedActorEntry(t, db, &pb.Entry{From: &pb.Feed{Id: "wanted"}})

	stats, err := backfillActorUUIDs(db, actorUUIDBackfillOptions{
		user:     "wanted",
		maxLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.entriesScanned != 1 || stats.entriesChanged != 1 {
		t.Fatalf("unexpected filtered stats: %+v", stats)
	}
}
