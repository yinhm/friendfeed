package model

import (
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
		{"", true},              // empty
		{"abc", true},           // too short
		{"UPPERCASE", true},     // uppercase
		{"has space", true},     // space
		{"has@special", true},   // special char
		{"valid", false},        // exactly 4 chars
		{"123_abc-def", false},  // mix of allowed chars
		{"User123", true},       // mixed case
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

	// Idempotent: renaming to the same ID is a no-op
	if err := RenameProfileId(db, profileUUID, "newname"); err != nil {
		t.Errorf("rename to same ID failed: %v", err)
	}

	// Uppercase input gets normalized
	if err := RenameProfileId(db, profileUUID, "FINAL"); err != nil {
		t.Fatalf("rename with uppercase: %v", err)
	}
	final, _ := GetProfileFromUserId(db, "final")
	if final == nil || final.Id != "final" {
		t.Error("uppercase ID was not normalized to lowercase")
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
