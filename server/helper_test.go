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
	if _, err := fmtEntryProfiles(db, entry); err != nil {
		t.Fatalf("fmtEntryProfiles after rename: %v", err)
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
	if _, err := fmtEntryProfiles(db, entry); err != nil {
		t.Fatalf("fmtEntryProfiles legacy: %v", err)
	}
	if entry.From.Id != "legacy" {
		t.Errorf("From.Id = %q; want legacy", entry.From.Id)
	}
	if entry.From.Name != "Legacy User" {
		t.Errorf("From.Name = %q; want %q", entry.From.Name, "Legacy User")
	}
}

// seedAuthorProfile seeds the entry author fmtEntryProfiles needs.
func seedAuthorProfile(t *testing.T, db *store.Store) uuid.UUID {
	t.Helper()
	authorUUID := uuid.Must(uuid.NewV4())
	if err := model.UpdateProfile(db, &pb.Profile{
		Uuid: authorUUID.String(), Id: "author", Name: "Author", Type: "user",
	}); err != nil {
		t.Fatalf("seed author: %v", err)
	}
	return authorUUID
}

func newAuthorEntry(authorUUID uuid.UUID) *pb.Entry {
	return &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: authorUUID.String(),
		From:        &pb.Feed{Uuid: authorUUID.String(), Id: "author"},
	}
}

// UUID-bearing comment/like refs refresh to the current profile after a
// rename and display-name change.
func TestFmtCommentOrLikeRefreshesUuidRefs(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()
	authorUUID := seedAuthorProfile(t, db)

	commenterUUID := uuid.Must(uuid.NewV4())
	if err := model.UpdateProfile(db, &pb.Profile{
		Uuid: commenterUUID.String(), Id: "oldcmt", Name: "Old Commenter", Type: "user",
	}); err != nil {
		t.Fatalf("seed commenter: %v", err)
	}
	if err := model.RenameProfileId(db, commenterUUID, "newcmt"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	renamed, err := model.GetProfileFromUuid(db, commenterUUID)
	if err != nil {
		t.Fatalf("fetch renamed: %v", err)
	}
	renamed.Name = "New Commenter"
	if err := model.UpdateProfile(db, renamed); err != nil {
		t.Fatalf("update name: %v", err)
	}

	snapshot := func() *pb.Feed {
		return &pb.Feed{Uuid: commenterUUID.String(), Id: "oldcmt", Name: "Old Commenter"}
	}
	entry := newAuthorEntry(authorUUID)
	entry.Comments = []*pb.Comment{{Id: uuid.Must(uuid.NewV4()).String(), From: snapshot()}}
	entry.Likes = []*pb.Like{{From: snapshot()}}

	if _, err := fmtEntryProfiles(db, entry); err != nil {
		t.Fatalf("fmtEntryProfiles: %v", err)
	}
	for _, ref := range []*pb.Feed{entry.Comments[0].From, entry.Likes[0].From} {
		if ref.Id != "newcmt" || ref.Name != "New Commenter" {
			t.Errorf("ref = <%q, %q>; want <newcmt, New Commenter>", ref.Id, ref.Name)
		}
	}
}

// A legacy ref WITHOUT a uuid keeps its snapshot even when its From.Id
// currently resolves to a real profile: the id may have been recycled,
// so hydrating by id could misattribute the record.
func TestFmtCommentOrLikeKeepsLegacySnapshot(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()
	authorUUID := seedAuthorProfile(t, db)

	// "newbie" is a real, current profile — a recycled id stand-in.
	if err := model.UpdateProfile(db, &pb.Profile{
		Uuid: uuid.Must(uuid.NewV4()).String(), Id: "newbie", Name: "Newbie", Type: "user",
	}); err != nil {
		t.Fatalf("seed recycled id: %v", err)
	}

	entry := newAuthorEntry(authorUUID)
	entry.Comments = []*pb.Comment{{
		Id:   uuid.Must(uuid.NewV4()).String(),
		From: &pb.Feed{Id: "newbie", Name: "Original Poster"}, // legacy, no uuid
	}}
	entry.Likes = []*pb.Like{{From: &pb.Feed{Id: "newbie", Name: "Original Liker"}}}

	if _, err := fmtEntryProfiles(db, entry); err != nil {
		t.Fatalf("fmtEntryProfiles: %v", err)
	}
	if got := entry.Comments[0].From; got.Id != "newbie" || got.Name != "Original Poster" {
		t.Errorf("comment ref = <%q, %q>; want snapshot kept, not refreshed to Newbie", got.Id, got.Name)
	}
	if got := entry.Likes[0].From; got.Id != "newbie" || got.Name != "Original Liker" {
		t.Errorf("like ref = <%q, %q>; want snapshot kept, not refreshed to Newbie", got.Id, got.Name)
	}
}

// Malformed uuids, unknown profiles, and nil refs are skipped quietly:
// the snapshot survives and the feed renders.
func TestFmtCommentOrLikeSkipsUnresolvable(t *testing.T) {
	db := store.NewStore(t.TempDir())
	defer db.Close()
	authorUUID := seedAuthorProfile(t, db)

	entry := newAuthorEntry(authorUUID)
	entry.Comments = []*pb.Comment{
		{Id: uuid.Must(uuid.NewV4()).String(), From: &pb.Feed{Uuid: "not-a-uuid", Id: "ghost", Name: "Ghost"}},
		{Id: uuid.Must(uuid.NewV4()).String(), From: &pb.Feed{Uuid: uuid.Must(uuid.NewV4()).String(), Id: "gone", Name: "Gone"}},
		{Id: uuid.Must(uuid.NewV4()).String(), From: nil},
	}
	entry.Likes = []*pb.Like{
		{From: &pb.Feed{Uuid: "not-a-uuid", Id: "ghost", Name: "Ghost"}},
		{From: nil},
	}

	if _, err := fmtEntryProfiles(db, entry); err != nil {
		t.Fatalf("fmtEntryProfiles: %v", err)
	}
	if got := entry.Comments[0].From; got.Id != "ghost" || got.Name != "Ghost" {
		t.Errorf("malformed uuid ref = <%q, %q>; want snapshot kept", got.Id, got.Name)
	}
	if got := entry.Comments[1].From; got.Id != "gone" || got.Name != "Gone" {
		t.Errorf("unknown profile ref = <%q, %q>; want snapshot kept", got.Id, got.Name)
	}
	if got := entry.Likes[0].From; got.Id != "ghost" || got.Name != "Ghost" {
		t.Errorf("malformed uuid like = <%q, %q>; want snapshot kept", got.Id, got.Name)
	}
}
