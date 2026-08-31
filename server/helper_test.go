package server

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

// TestFmtEntryProfileSurvivesRename reproduces the feed 404 that occurred
// after a profile ID rename: historical entries carry a denormalized
// From.Id snapshot ("yinhm"), so once the profile is renamed to "yinhm2"
// the old id->uuid mapping is gone. Resolving by From.Id would fail; the
// fix resolves by the stable ProfileUuid and refreshes From.Id.
func TestFmtEntryProfileSurvivesRename(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	profileUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{
		Uuid:    profileUUID.String(),
		Id:      "oldname",
		Name:    "Test User",
		Type:    "user",
		Picture: "http://example.com/new.jpg",
		Private: true,
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
	if entry.From.Uuid != profileUUID.String() {
		t.Errorf("From.Uuid = %q; want %q", entry.From.Uuid, profileUUID.String())
	}
	if entry.From.Type != "user" {
		t.Errorf("From.Type = %q; want %q", entry.From.Type, "user")
	}
	if !entry.From.Private {
		t.Error("From.Private = false; want current Profile privacy")
	}
}

// TestFmtEntryProfileLegacyFallback covers entries without a ProfileUuid
// (older data): they must still resolve via From.Id.
func TestFmtEntryProfileLegacyFallback(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
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
	if entry.From.Type != "user" {
		t.Errorf("From.Type = %q; want %q", entry.From.Type, "user")
	}
	// The profile was resolved through the recyclable id, so the stable
	// identity field must NOT be stamped from it.
	if entry.From.Uuid != "" {
		t.Errorf("From.Uuid = %q; want empty (no stamping via legacy id fallback)", entry.From.Uuid)
	}
}

// A legacy entry whose From.Id has been recycled by another user must
// not have the current registrant's UUID attributed to it: display
// fields may refresh for compatibility, but the identity field stays
// untouched.
func TestFmtEntryProfilesLegacyRecycledIdNoUuidStamp(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	registrantUUID := uuid.Must(uuid.NewV4())
	if err := model.UpdateProfile(db, &pb.Profile{
		Uuid: registrantUUID.String(), Id: "newbie", Name: "Newbie", Type: "user",
	}); err != nil {
		t.Fatalf("seed registrant: %v", err)
	}

	// Legacy entry from the ORIGINAL owner of "newbie": no ProfileUuid,
	// no From.Uuid — only the recyclable id snapshot.
	entry := &pb.Entry{
		Id:   uuid.Must(uuid.NewV4()).String(),
		From: &pb.Feed{Id: "newbie", Name: "Original Author"},
	}
	if _, err := fmtEntryProfiles(db, entry); err != nil {
		t.Fatalf("fmtEntryProfiles: %v", err)
	}
	if entry.From.Uuid == registrantUUID.String() {
		t.Errorf("From.Uuid stamped with the current registrant %q; identity misattribution", registrantUUID)
	}
	if entry.From.Uuid != "" {
		t.Errorf("From.Uuid = %q; want empty", entry.From.Uuid)
	}
}

// An entry with a nil From must gain a complete canonical reference,
// including Uuid and Type.
func TestFmtEntryProfilesNilFrom(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	profileUUID := uuid.Must(uuid.NewV4())
	if err := model.UpdateProfile(db, &pb.Profile{
		Uuid: profileUUID.String(), Id: "author", Name: "Author", Type: "user",
	}); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	entry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: profileUUID.String(),
		// no From at all
	}
	if _, err := fmtEntryProfiles(db, entry); err != nil {
		t.Fatalf("fmtEntryProfiles nil From: %v", err)
	}
	from := entry.From
	if from == nil {
		t.Fatal("From still nil")
	}
	if from.Uuid != profileUUID.String() || from.Id != "author" ||
		from.Name != "Author" || from.Type != "user" {
		t.Errorf("From = <%q, %q, %q, %q>; want full canonical ref",
			from.Uuid, from.Id, from.Name, from.Type)
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

func TestProfileResolverCachesStableUUIDLookup(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	profileUUID := seedAuthorProfile(t, db)
	resolver := newProfileResolver(db)

	first, err := resolver.profile(profileUUID)
	if err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	updated := proto.Clone(first).(*pb.Profile)
	updated.Name = "Updated after first lookup"
	if err := model.UpdateProfile(db, updated); err != nil {
		t.Fatalf("update profile: %v", err)
	}

	second, err := resolver.profile(profileUUID)
	if err != nil {
		t.Fatalf("second lookup: %v", err)
	}
	if second != first {
		t.Fatal("same request returned a different cached profile pointer")
	}
	if second.Name != "Author" {
		t.Fatalf("cached name = %q; want first lookup snapshot", second.Name)
	}

	fresh, err := newProfileResolver(db).profile(profileUUID)
	if err != nil {
		t.Fatalf("fresh request lookup: %v", err)
	}
	if fresh.Name != updated.Name {
		t.Fatalf("fresh request name = %q; want %q", fresh.Name, updated.Name)
	}
}

func TestProfileResolverCachesNotFound(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	profileUUID := uuid.Must(uuid.NewV4())
	resolver := newProfileResolver(db)
	if _, err := resolver.profile(profileUUID); err == nil {
		t.Fatal("first missing lookup returned no error")
	}
	if err := model.UpdateProfile(db, &pb.Profile{
		Uuid: profileUUID.String(),
		Id:   "created-after-miss",
		Name: "Created After Miss",
		Type: "user",
	}); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if _, err := resolver.profile(profileUUID); err == nil {
		t.Fatal("same request did not retain the cached NotFound result")
	}
	if _, err := newProfileResolver(db).profile(profileUUID); err != nil {
		t.Fatalf("fresh request did not see created profile: %v", err)
	}
}

func TestProfileResolverRejectsZeroUUIDWithoutLookup(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	resolver := newProfileResolver(db)
	defer resolver.mdb.Close()

	if _, err := resolver.profile(uuid.Nil); err == nil {
		t.Fatal("zero UUID returned no error")
	}
	if len(resolver.results) != 0 {
		t.Fatalf("resolver cached %d results; zero UUID must not be queried", len(resolver.results))
	}
}

// An entry whose ProfileUuid is the zero UUID must be rejected: the
// zero UUID parses but is not an identity, even if an abnormal
// zero-uuid profile exists in the database.
func TestFmtEntryProfilesRejectsZeroProfileUuid(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	if err := model.UpdateProfile(db, &pb.Profile{
		Uuid: uuid.Nil.String(), Id: "zeroprofile", Name: "Zero Profile", Type: "user",
	}); err != nil {
		t.Fatalf("seed zero-uuid profile: %v", err)
	}

	entry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: uuid.Nil.String(),
		From:        &pb.Feed{Id: "original", Name: "Original"},
	}
	if _, err := fmtEntryProfiles(db, entry); err == nil {
		t.Fatal("zero ProfileUuid must be rejected")
	}
	if entry.From.Id != "original" || entry.From.Name != "Original" || entry.From.Uuid != "" {
		t.Errorf("From = <%q, %q, %q>; want untouched", entry.From.Id, entry.From.Name, entry.From.Uuid)
	}
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
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
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
	renamed.Picture = "http://example.com/newcmt.jpg"
	renamed.Private = true
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
		if ref.Picture != "http://example.com/newcmt.jpg" || ref.Type != "user" {
			t.Errorf("ref snapshot = <%q, %q>; want refreshed picture and type", ref.Picture, ref.Type)
		}
		if !ref.Private {
			t.Error("ref.Private = false; want current Profile privacy")
		}
	}
}

// The zero UUID parses but is not a valid identity: a zero-uuid ref
// keeps its snapshot even if a zero-uuid profile somehow exists.
func TestFmtCommentOrLikeRejectsZeroUuid(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	authorUUID := seedAuthorProfile(t, db)

	// Abnormal data: a profile keyed by the zero UUID.
	if err := model.UpdateProfile(db, &pb.Profile{
		Uuid: uuid.Nil.String(), Id: "zeroprofile", Name: "Zero Profile", Type: "user",
	}); err != nil {
		t.Fatalf("seed zero-uuid profile: %v", err)
	}

	entry := newAuthorEntry(authorUUID)
	entry.Comments = []*pb.Comment{{
		Id:   uuid.Must(uuid.NewV4()).String(),
		From: &pb.Feed{Uuid: uuid.Nil.String(), Id: "ghost", Name: "Ghost"},
	}}

	if _, err := fmtEntryProfiles(db, entry); err != nil {
		t.Fatalf("fmtEntryProfiles: %v", err)
	}
	if got := entry.Comments[0].From; got.Id != "ghost" || got.Name != "Ghost" {
		t.Errorf("zero-uuid ref = <%q, %q>; want snapshot kept, not refreshed to Zero Profile", got.Id, got.Name)
	}
}

// A legacy ref WITHOUT a uuid keeps its snapshot even when its From.Id
// currently resolves to a real profile: the id may have been recycled,
// so hydrating by id could misattribute the record.
func TestFmtCommentOrLikeKeepsLegacySnapshot(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
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
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
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

// An entry whose author profile no longer resolves (deleted or archived
// data) must still hydrate its comment/like refs: they carry their own
// stable UUIDs. The author error is returned for strict callers, but
// lenient paths render the hydrated rest.
func TestFmtEntryProfilesHydratesRefsWhenAuthorMissing(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	commenterUUID := uuid.Must(uuid.NewV4())
	if err := model.UpdateProfile(db, &pb.Profile{
		Uuid: commenterUUID.String(), Id: "commenter", Name: "Real Commenter", Type: "user",
	}); err != nil {
		t.Fatalf("seed commenter: %v", err)
	}

	entry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: uuid.Must(uuid.NewV4()).String(), // no such profile
		From:        &pb.Feed{Id: "bot", Name: "Bot"},
		Comments: []*pb.Comment{{
			Id:   uuid.Must(uuid.NewV4()).String(),
			From: &pb.Feed{Uuid: commenterUUID.String(), Id: "oldsnap", Name: "Old Snapshot"},
		}},
		Likes: []*pb.Like{{
			From: &pb.Feed{Uuid: commenterUUID.String(), Id: "oldsnap", Name: "Old Snapshot"},
		}},
	}

	if _, err := fmtEntryProfiles(db, entry); err == nil {
		t.Fatal("missing author must still return an error")
	}
	// Author snapshot untouched...
	if entry.From.Id != "bot" || entry.From.Name != "Bot" {
		t.Errorf("author From = <%q, %q>; want snapshot kept", entry.From.Id, entry.From.Name)
	}
	// ...but comment and like refs hydrated.
	if got := entry.Comments[0].From; got.Id != "commenter" || got.Name != "Real Commenter" {
		t.Errorf("comment From = <%q, %q>; want hydrated", got.Id, got.Name)
	}
	if got := entry.Likes[0].From; got.Id != "commenter" || got.Name != "Real Commenter" {
		t.Errorf("like From = <%q, %q>; want hydrated", got.Id, got.Name)
	}
}
