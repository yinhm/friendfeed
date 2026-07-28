package model

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestValidateProfileId(t *testing.T) {
	tests := []struct {
		id      string
		wantErr bool
	}{
		{"valid", false},
		{"valid123", false},
		{"valid_name", false},
		{"valid-name", false},
		{"a1b2", false},
		{"", true},             // empty
		{"abc", true},          // too short
		{"UPPERCASE", true},    // uppercase
		{"has space", true},    // space
		{"has@special", true},  // special char
		{"valid", false},       // exactly 4 chars
		{"123_abc-def", false}, // mix of allowed chars
		{"User123", true},      // mixed case
	}
	for _, tt := range tests {
		err := ValidateProfileId(tt.id)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateProfileId(%q) error = %v; wantErr %t", tt.id, err, tt.wantErr)
		}
	}
}

func TestNormalizeProfileId(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"Valid", "valid", false},
		{"UsErNaMe", "username", false},
		{"SHOUT", "shout", false},
		{"valid", "valid", false},
		{"AB", "", true}, // too short after normalize
	}
	for _, tt := range tests {
		got, err := NormalizeProfileId(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("NormalizeProfileId(%q) error = %v; wantErr %t", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeProfileId(%q) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenameProfileId(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	profileUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{
		Uuid: profileUUID.String(),
		Id:   "oldname",
		Name: "Test User",
		Type: "user",
	}
	if err := UpdateProfile(db, profile); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	// Rename to a valid new ID
	if err := RenameProfileId(db, profileUUID, "newname"); err != nil {
		t.Fatalf("RenameProfileId: %v", err)
	}

	// Verify old ID no longer resolves
	if _, err := GetProfileFromUserId(db, "oldname"); err == nil {
		t.Error("old ID still resolves after rename")
	}

	// Verify new ID resolves to the same profile
	renamed, err := GetProfileFromUserId(db, "newname")
	if err != nil {
		t.Fatalf("new ID does not resolve: %v", err)
	}
	if renamed.Uuid != profileUUID.String() || renamed.Id != "newname" {
		t.Errorf("renamed profile = (Uuid=%q, Id=%q); want (Uuid=%q, Id=%q)",
			renamed.Uuid, renamed.Id, profileUUID.String(), "newname")
	}
	renameRaw, err := UserRenameMap.GetRaw(db, []byte("oldname"))
	if err != nil || !bytes.Equal(renameRaw, profileUUID.Bytes()) {
		t.Fatalf("UserRenameMap = %x, %v; want %x", renameRaw, err, profileUUID.Bytes())
	}
	softRenamed, err := GetProfileFromRenameId(db, "oldname")
	if err != nil || softRenamed.Id != "newname" || softRenamed.Uuid != profileUUID.String() {
		t.Fatalf("soft rename lookup = %v, %v; want current newname profile", softRenamed, err)
	}

	// Idempotent: renaming to the same ID is a no-op
	if err := RenameProfileId(db, profileUUID, "newname"); err != nil {
		t.Errorf("rename to same ID failed: %v", err)
	}

	// A profile may change its ID only once.
	if err := RenameProfileId(db, profileUUID, "FINAL"); err == nil {
		t.Fatal("second rename unexpectedly succeeded")
	}
	stillRenamed, err := GetProfileFromUserId(db, "newname")
	if err != nil || stillRenamed.Id != "newname" {
		t.Fatalf("second rename changed profile: %v, %v", stillRenamed, err)
	}

	// Collision: create a second profile and try to rename to its ID
	otherUUID := uuid.Must(uuid.NewV4())
	other := &pb.Profile{
		Uuid: otherUUID.String(),
		Id:   "taken",
		Name: "Other User",
		Type: "user",
	}
	if err := UpdateProfile(db, other); err != nil {
		t.Fatalf("seed second profile: %v", err)
	}
	if err := RenameProfileId(db, profileUUID, "taken"); err == nil {
		t.Error("expected collision error when renaming to an existing ID")
	}

	// Invalid ID format should fail
	if err := RenameProfileId(db, profileUUID, "ab"); err == nil {
		t.Error("expected validation error for ID shorter than 4 chars")
	}
	if err := RenameProfileId(db, profileUUID, "has space"); err == nil {
		t.Error("expected validation error for ID with space")
	}
}

func TestRenameProfileIdRejectsReservedPreviousID(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	firstUUID := uuid.Must(uuid.NewV4())
	secondUUID := uuid.Must(uuid.NewV4())
	for _, profile := range []*pb.Profile{
		{Uuid: firstUUID.String(), Id: "first-old", Name: "First", Type: "user"},
		{Uuid: secondUUID.String(), Id: "second-old", Name: "Second", Type: "user"},
	} {
		if err := UpdateProfile(db, profile); err != nil {
			t.Fatal(err)
		}
	}
	if err := RenameProfileId(db, firstUUID, "first-new"); err != nil {
		t.Fatal(err)
	}
	if err := RenameProfileId(db, secondUUID, "first-old"); err == nil {
		t.Fatal("rename reused a reserved previous ID")
	}
	thirdUUID := uuid.Must(uuid.NewV4())
	if err := UpdateProfile(db, &pb.Profile{
		Uuid: thirdUUID.String(),
		Id:   "first-old",
		Name: "Third",
		Type: "user",
	}); err == nil {
		t.Fatal("profile creation reused a reserved previous ID")
	}
	second, err := GetProfileFromUserId(db, "second-old")
	if err != nil || second.Uuid != secondUUID.String() {
		t.Fatalf("rejected profile changed: %v, %v", second, err)
	}
}

func TestRenameProfileIdNormalizesUppercaseInput(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	profileUUID := uuid.Must(uuid.NewV4())
	if err := UpdateProfile(db, &pb.Profile{
		Uuid: profileUUID.String(),
		Id:   "before",
		Name: "Before",
		Type: "user",
	}); err != nil {
		t.Fatal(err)
	}
	if err := RenameProfileId(db, profileUUID, "AFTER"); err != nil {
		t.Fatalf("rename uppercase ID: %v", err)
	}
	profile, err := GetProfileFromUserId(db, "after")
	if err != nil || profile.Id != "after" {
		t.Fatalf("normalized profile = %v, %v; want id after", profile, err)
	}
}

func TestRenameProfileIdRejectsCorruptCollisionMapWithoutMutation(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	profileUUID := uuid.Must(uuid.NewV4())
	if err := UpdateProfile(db, &pb.Profile{
		Uuid: profileUUID.String(),
		Id:   "oldname",
		Name: "Test User",
		Type: "user",
	}); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	targetKey := NewKeyFrom(TableUserMap.Bytes(), []byte("newname"))
	corrupt := []byte("not-a-uuid")
	if err := db.Put(targetKey, corrupt); err != nil {
		t.Fatalf("seed corrupt map: %v", err)
	}

	err := RenameProfileId(db, profileUUID, "newname")
	if err == nil || !strings.Contains(err.Error(), "decode UserMap") {
		t.Fatalf("RenameProfileId error = %v; want decode UserMap error", err)
	}

	old, err := GetProfileFromUserId(db, "oldname")
	if err != nil || old.Id != "oldname" {
		t.Fatalf("old profile = %v, %v; want unchanged", old, err)
	}
	stored, err := GetProfileFromUuid(db, profileUUID)
	if err != nil || stored.Id != "oldname" {
		t.Fatalf("stored profile = %v, %v; want oldname", stored, err)
	}
	got, err := db.Get(targetKey)
	if err != nil || string(got) != string(corrupt) {
		t.Fatalf("target map = %q, %v; want corrupt value untouched", got, err)
	}
}

func TestRenameProfileIdRejectsMismatchedOldMapWithoutMutation(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	profileUUID := uuid.Must(uuid.NewV4())
	if err := UpdateProfile(db, &pb.Profile{
		Uuid: profileUUID.String(),
		Id:   "oldname",
		Name: "Test User",
		Type: "user",
	}); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	otherUUID := uuid.Must(uuid.NewV4())
	oldMapKey := NewKeyFrom(TableUserMap.Bytes(), []byte("oldname"))
	if err := db.Put(oldMapKey, otherUUID.Bytes()); err != nil {
		t.Fatalf("corrupt old map: %v", err)
	}

	err := RenameProfileId(db, profileUUID, "newname")
	if err == nil || !strings.Contains(err.Error(), "belongs to another profile") {
		t.Fatalf("RenameProfileId error = %v; want old-map ownership error", err)
	}

	stored, err := GetProfileFromUuid(db, profileUUID)
	if err != nil || stored.Id != "oldname" {
		t.Fatalf("stored profile = %v, %v; want oldname", stored, err)
	}
	if got, err := db.Get(oldMapKey); err != nil || string(got) != string(otherUUID.Bytes()) {
		t.Fatalf("old map = %x, %v; want other UUID untouched", got, err)
	}
	newMapKey := NewKeyFrom(TableUserMap.Bytes(), []byte("newname"))
	if got, err := db.Get(newMapKey); err != nil || got != nil {
		t.Fatalf("new map = %x, %v; want missing", got, err)
	}
}

func TestRenameProfileIdSerializesConcurrentCollision(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	firstUUID := uuid.Must(uuid.NewV4())
	secondUUID := uuid.Must(uuid.NewV4())
	for _, profile := range []*pb.Profile{
		{Uuid: firstUUID.String(), Id: "first-user", Name: "First", Type: "user"},
		{Uuid: secondUUID.String(), Id: "second-user", Name: "Second", Type: "user"},
	} {
		if err := UpdateProfile(db, profile); err != nil {
			t.Fatalf("seed %s: %v", profile.Id, err)
		}
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, profileUUID := range []uuid.UUID{firstUUID, secondUUID} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- RenameProfileId(db, profileUUID, "shared-name")
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	failures := 0
	for err := range errs {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("rename results = %d success, %d failure; want 1 and 1", successes, failures)
	}

	winner, err := GetProfileFromUserId(db, "shared-name")
	if err != nil {
		t.Fatalf("shared-name: %v", err)
	}
	if winner.Uuid != firstUUID.String() && winner.Uuid != secondUUID.String() {
		t.Fatalf("winner UUID = %q; want one contender", winner.Uuid)
	}

	loserID := "first-user"
	if winner.Uuid == firstUUID.String() {
		loserID = "second-user"
	}
	loser, err := GetProfileFromUserId(db, loserID)
	if err != nil {
		t.Fatalf("loser mapping %s: %v", loserID, err)
	}
	if loser.Id != loserID {
		t.Fatalf("loser profile ID = %q; want %q", loser.Id, loserID)
	}
}

func TestRenameProfileIdRejectsZeroUuid(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	if err := RenameProfileId(db, uuid.Nil, "newname"); err == nil {
		t.Fatal("zero profile UUID must be rejected")
	}
}
