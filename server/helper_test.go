package server

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

// TestFmtEntryProfileSurvivesRename reproduces the feed 404 that occurred
// after a profile ID rename: historical entries carry a denormalized
// From.Id snapshot ("yinhm"), so once the profile is renamed to "yinhm2"
// the old id->uuid mapping is gone. Resolving by From.Id would fail; the
// fix resolves by the stable ProfileUuid and refreshes From.Id.
func TestFmtEntryProfileSurvivesRename(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	profileUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{
		Uuid:    profileUUID.String(),
		Id:      "oldname",
		Name:    "Test User",
		Type:    "user",
		Picture: "http://example.com/new.jpg",
	}
	if err := model.UpdateProfile(db, profile); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	// Entry posted before the rename: From.Id holds the old snapshot.
	entry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: profileUUID.String(),
		From:        &pb.Feed{Id: "oldname", Name: "Test User", Uuid: profileUUID.String()},
	}

	// Rename the profile ID; the old id->uuid mapping disappears.
	if err := model.RenameProfileId(db, profileUUID, "newname"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := model.GetProfileFromUserId(db, "oldname"); err == nil {
		t.Fatal("precondition failed: old id still resolves")
	}

	// The profile edit page may also update the display name.
	renamed, err := model.GetProfileFromUuid(db, profileUUID)
	if err != nil {
		t.Fatalf("fetch renamed profile: %v", err)
	}
	renamed.Name = "Renamed User"
	if err := model.UpdateProfile(db, renamed); err != nil {
		t.Fatalf("update name: %v", err)
	}

	// Formatting the historical entry must succeed and refresh From fields.
	if _, err := fmtEntryProfile(db, entry); err != nil {
		t.Fatalf("fmtEntryProfile after rename: %v", err)
	}
	if entry.From.Id != "newname" {
		t.Errorf("From.Id = %q; want %q (should refresh to current id)", entry.From.Id, "newname")
	}
	if entry.From.Name != "Renamed User" {
		t.Errorf("From.Name = %q; want %q (should refresh to current name)", entry.From.Name, "Renamed User")
	}
	if entry.From.Picture != "http://example.com/new.jpg" {
		t.Errorf("From.Picture = %q; want refreshed picture", entry.From.Picture)
	}
}

// TestFmtEntryProfileLegacyFallback covers entries without a ProfileUuid
// (older data): they must still resolve via From.Id.
func TestFmtEntryProfileLegacyFallback(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()

	profileUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{
		Uuid: profileUUID.String(),
		Id:   "legacy",
		Name: "Legacy User",
		Type: "user",
	}
	if err := model.UpdateProfile(db, profile); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	entry := &pb.Entry{
		Id:   uuid.Must(uuid.NewV4()).String(),
		From: &pb.Feed{Id: "legacy"},
		// no ProfileUuid
	}
	if _, err := fmtEntryProfile(db, entry); err != nil {
		t.Fatalf("fmtEntryProfile legacy: %v", err)
	}
	if entry.From.Id != "legacy" {
		t.Errorf("From.Id = %q; want legacy", entry.From.Id)
	}
	if entry.From.Name != "Legacy User" {
		t.Errorf("From.Name = %q; want %q", entry.From.Name, "Legacy User")
	}
}
