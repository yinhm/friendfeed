package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

// Target permission rules for comment/like mutations, locked by these tests:
//   - edit comment: the comment author only, verified by stable UUID;
//   - delete comment: the comment author, the entry author (resolved via
//     entry.ProfileUuid), or a super admin;
//   - like: no duplicates per user; unlike removes only the caller's like;
//   - identity is UUID-only: legacy UUID-less, malformed-UUID, nil and
//     recycled-id references never authorize and never panic.

var (
	likeTestOwnerUUID  = uuid.Must(uuid.NewV4())
	likeTestOtherUUID  = uuid.Must(uuid.NewV4())
	likeTestEntryUUID  = uuid.Must(uuid.NewV4())
	likeTestSuperUUID  = uuid.Must(uuid.NewV4())
	likeTestCommentID  = uuid.Must(uuid.NewV4()).String()
	likeTestProfileFor = func(id string, u uuid.UUID) *pb.Profile {
		return &pb.Profile{Uuid: u.String(), Id: id, Name: id, Type: "user"}
	}
)

func likeTestDB(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// newLikeTestEntry returns an entry authored by likeTestEntryUUID.
func newLikeTestEntry() *pb.Entry {
	return &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: likeTestEntryUUID.String(),
		Date:        time.Now().UTC().Format(time.RFC3339),
	}
}

// ownerComment returns the target-shape comment: stable UUID plus the
// id/name snapshot taken when it was written.
func ownerComment() *pb.Comment {
	return &pb.Comment{
		Id:   likeTestCommentID,
		Date: time.Now().UTC().Format(time.RFC3339),
		Body: "original body",
		From: &pb.Feed{Uuid: likeTestOwnerUUID.String(), Id: "owner", Name: "Owner"},
	}
}

func ownerLike() *pb.Like {
	return &pb.Like{
		Date: time.Now().UTC().Format(time.RFC3339),
		From: &pb.Feed{Uuid: likeTestOwnerUUID.String(), Id: "owner", Name: "Owner"},
	}
}

// editBy builds the edit request a client sends: same comment id, the
// caller's current actor reference, new body.
func editBy(profile *pb.Profile, body string) *pb.Comment {
	return &pb.Comment{
		Id:   likeTestCommentID,
		Body: body,
		From: &pb.Feed{Uuid: profile.Uuid, Id: profile.Id, Name: profile.Name},
	}
}

func mustNotPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", what, r)
		}
	}()
	f()
}

func TestCommentOwnerCanEdit(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)

	entry := newLikeTestEntry()
	entry.Comments = []*pb.Comment{ownerComment()}

	_, entry, err := Comment(db, owner, entry, editBy(owner, "edited body"))
	if err != nil {
		t.Fatalf("owner edit: %v", err)
	}
	if entry.Comments[0].Body != "edited body" {
		t.Errorf("body = %q; want edited body", entry.Comments[0].Body)
	}
}

// After a rename the stored From.Id is stale; ownership is verified by
// the stable From.Uuid, so the author can still edit their own comment.
func TestCommentOwnerCanEditAfterRename(t *testing.T) {
	db := likeTestDB(t)
	renamed := likeTestProfileFor("newowner", likeTestOwnerUUID)

	entry := newLikeTestEntry()
	entry.Comments = []*pb.Comment{ownerComment()}

	if _, _, err := Comment(db, renamed, entry, editBy(renamed, "edited body")); err != nil {
		t.Fatalf("renamed owner edit: %v", err)
	}
}

func TestCommentEditForbiddenForOthers(t *testing.T) {
	db := likeTestDB(t)
	other := likeTestProfileFor("other", likeTestOtherUUID)

	entry := newLikeTestEntry()
	entry.Comments = []*pb.Comment{ownerComment()}

	if _, _, err := Comment(db, other, entry, editBy(other, "hijack")); err == nil {
		t.Fatal("other user edit must be rejected")
	}
}

// Edit is author-only: even the entry author and super admins, who may
// delete comments for moderation, must not edit other users' comments.
func TestPrivilegedUsersCannotEditOthersComment(t *testing.T) {
	entryAuthor := likeTestProfileFor("entry", likeTestEntryUUID)
	super := likeTestProfileFor("super", likeTestSuperUUID)
	super.IsSuper = true

	cases := map[string]*pb.Profile{
		"entry author": entryAuthor,
		"super":        super,
	}
	for name, caller := range cases {
		t.Run(name, func(t *testing.T) {
			db := likeTestDB(t)
			entry := newLikeTestEntry()
			entry.Comments = []*pb.Comment{ownerComment()}

			if _, _, err := Comment(db, caller, entry, editBy(caller, "hijack")); err == nil {
				t.Errorf("%s must not edit another user's comment", name)
			}
		})
	}
}

func TestCommentOwnerCanDelete(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)

	entry := newLikeTestEntry()
	entry.Comments = []*pb.Comment{ownerComment()}

	entry, err := DeleteComment(db, owner, entry, likeTestCommentID)
	if err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if len(entry.Comments) != 0 {
		t.Errorf("comments = %d; want 0", len(entry.Comments))
	}
}

// The entry author moderates their own entry: they may delete another
// user's comment on it (but not edit it).
func TestEntryAuthorCanDeleteComment(t *testing.T) {
	db := likeTestDB(t)
	entryAuthor := likeTestProfileFor("entry", likeTestEntryUUID)

	entry := newLikeTestEntry()
	entry.Comments = []*pb.Comment{ownerComment()}

	entry, err := DeleteComment(db, entryAuthor, entry, likeTestCommentID)
	if err != nil {
		t.Fatalf("entry author delete: %v", err)
	}
	if len(entry.Comments) != 0 {
		t.Errorf("comments = %d; want 0", len(entry.Comments))
	}
}

func TestSuperCanDeleteComment(t *testing.T) {
	db := likeTestDB(t)
	super := likeTestProfileFor("super", likeTestSuperUUID)
	super.IsSuper = true

	entry := newLikeTestEntry()
	entry.Comments = []*pb.Comment{ownerComment()}

	entry, err := DeleteComment(db, super, entry, likeTestCommentID)
	if err != nil {
		t.Fatalf("super delete: %v", err)
	}
	if len(entry.Comments) != 0 {
		t.Errorf("comments = %d; want 0", len(entry.Comments))
	}
}

// An unrelated user must not delete another user's comment: delete
// requires comment author, entry author (entry.ProfileUuid) or super.
func TestOtherUserCannotDeleteComment(t *testing.T) {
	db := likeTestDB(t)
	other := likeTestProfileFor("other", likeTestOtherUUID)

	entry := newLikeTestEntry()
	entry.Comments = []*pb.Comment{ownerComment()}

	entry, err := DeleteComment(db, other, entry, likeTestCommentID)
	if err == nil {
		t.Error("other user delete must be rejected")
	}
	if len(entry.Comments) != 1 {
		t.Errorf("comments = %d; want 1 (comment must survive)", len(entry.Comments))
	}
}

func TestLikeNotDuplicated(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)

	entry := newLikeTestEntry()
	entry.Likes = []*pb.Like{ownerLike()}

	_, entry, err := Like(db, owner, entry)
	if err != nil {
		t.Fatalf("Like: %v", err)
	}
	if len(entry.Likes) != 1 {
		t.Errorf("likes = %d; want 1 (no duplicate)", len(entry.Likes))
	}
}

// New likes/comments persist the canonical actor reference (stable UUID plus
// display snapshot), never caller-supplied identity fields.
func TestLikeStoresCanonicalActorRef(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)
	owner.Picture = "http://example.com/o.jpg"

	_, entry, err := Like(db, owner, newLikeTestEntry())
	if err != nil {
		t.Fatalf("Like: %v", err)
	}
	if len(entry.Likes) != 1 {
		t.Fatalf("likes = %d; want 1", len(entry.Likes))
	}
	from := entry.Likes[0].From
	if from.Uuid != owner.Uuid {
		t.Errorf("From.Uuid = %q; want %q", from.Uuid, owner.Uuid)
	}
	if from.Id != "owner" || from.Name != "owner" || from.Picture != "http://example.com/o.jpg" {
		t.Errorf("snapshot = <%q, %q, %q>; want copied from profile", from.Id, from.Name, from.Picture)
	}
}

func TestLikeRejectsProfileWithoutIdentity(t *testing.T) {
	db := likeTestDB(t)
	if _, _, err := Like(db, &pb.Profile{Id: "nouuid"}, newLikeTestEntry()); err == nil {
		t.Fatal("Like with uuid-less profile must fail")
	}
}

// The identity mint must not be bypassed by a dedupe hit: even when an
// existing like's From.Id already matches, invalid profiles are
// rejected (and a nil profile fails cleanly instead of panicking).
func TestLikeValidatesProfileBeforeDedupe(t *testing.T) {
	db := likeTestDB(t)
	entry := newLikeTestEntry()
	entry.Likes = []*pb.Like{{From: &pb.Feed{Id: "owner"}}}

	cases := map[string]*pb.Profile{
		"empty uuid":     {Id: "owner"},
		"malformed uuid": {Uuid: "not-a-uuid", Id: "owner"},
		"zero uuid":      {Uuid: uuid.Nil.String(), Id: "owner"},
	}
	for name, profile := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Like(db, profile, entry); err == nil {
				t.Error("Like must reject the profile even on a dedupe hit")
			}
		})
	}

	t.Run("nil profile", func(t *testing.T) {
		var err error
		mustNotPanic(t, "Like", func() { _, _, err = Like(db, nil, entry) })
		if err == nil {
			t.Error("Like with nil profile must fail")
		}
	})
}

// Comment validates the principal before scanning existing comments, so
// an invalid profile is rejected as such, not masked by a 403 or an
// append.
func TestCommentValidatesProfileFirst(t *testing.T) {
	db := likeTestDB(t)
	entry := newLikeTestEntry()
	entry.Comments = []*pb.Comment{ownerComment()}

	if _, _, err := Comment(db, &pb.Profile{Id: "owner"}, entry, &pb.Comment{
		Id:   uuid.Must(uuid.NewV4()).String(),
		Body: "hi",
	}); err == nil {
		t.Fatal("Comment with uuid-less profile must fail")
	}

	var err error
	mustNotPanic(t, "Comment", func() {
		_, _, err = Comment(db, nil, entry, &pb.Comment{Id: uuid.Must(uuid.NewV4()).String(), Body: "hi"})
	})
	if err == nil {
		t.Fatal("Comment with nil profile must fail")
	}
}

func TestCommentStoresCanonicalActorRef(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)

	// The caller forges From as another user; the stored comment must
	// carry the canonical principal's identity instead.
	forged := &pb.Comment{
		Id:   uuid.Must(uuid.NewV4()).String(),
		Body: "hello",
		From: &pb.Feed{Uuid: likeTestOtherUUID.String(), Id: "other", Name: "Other"},
	}
	_, entry, err := Comment(db, owner, newLikeTestEntry(), forged)
	if err != nil {
		t.Fatalf("Comment: %v", err)
	}
	if len(entry.Comments) != 1 {
		t.Fatalf("comments = %d; want 1", len(entry.Comments))
	}
	from := entry.Comments[0].From
	if from.Uuid != owner.Uuid || from.Id != "owner" || from.Name != "owner" {
		t.Errorf("From = <%q, %q, %q>; want canonical owner", from.Uuid, from.Id, from.Name)
	}
}

// The stored like carries the author's stable UUID with a stale From.Id;
// dedupe by UUID must still recognize it after a rename.
func TestLikeNotDuplicatedAfterRename(t *testing.T) {
	db := likeTestDB(t)
	renamed := likeTestProfileFor("newowner", likeTestOwnerUUID)

	entry := newLikeTestEntry()
	entry.Likes = []*pb.Like{ownerLike()}

	_, entry, err := Like(db, renamed, entry)
	if err != nil {
		t.Fatalf("Like: %v", err)
	}
	if len(entry.Likes) != 1 {
		t.Errorf("likes = %d; want 1 (no duplicate after rename)", len(entry.Likes))
	}
}

func TestUnlikeKeepsOthersLikes(t *testing.T) {
	db := likeTestDB(t)
	other := likeTestProfileFor("other", likeTestOtherUUID)

	entry := newLikeTestEntry()
	entry.Likes = []*pb.Like{ownerLike()}

	entry, err := DeleteLike(db, other, entry)
	if err != nil {
		t.Fatalf("DeleteLike: %v", err)
	}
	if len(entry.Likes) != 1 {
		t.Errorf("likes = %d; want 1 (other user's like untouched)", len(entry.Likes))
	}
}

// Unlike locates the like by stable UUID, so the author can remove
// their own like even after a rename.
func TestUnlikeAfterRename(t *testing.T) {
	db := likeTestDB(t)
	renamed := likeTestProfileFor("newowner", likeTestOwnerUUID)

	entry := newLikeTestEntry()
	entry.Likes = []*pb.Like{ownerLike()}

	entry, err := DeleteLike(db, renamed, entry)
	if err != nil {
		t.Fatalf("DeleteLike: %v", err)
	}
	if len(entry.Likes) != 0 {
		t.Errorf("likes = %d; want 0 (own like removed after rename)", len(entry.Likes))
	}
}

// The core no-fallback contract: a caller whose current ID happens to
// match the stored From.Id snapshot but whose UUID differs (e.g. the ID
// was recycled after a rename) must gain NOTHING from the ID match.
// The current implementation must compare UUIDs only, so the impostor
// gains nothing from the recycled id.
func TestSameIdDifferentUuidNeverAuthorizes(t *testing.T) {
	// Impostor currently owns the id "owner" but is a different profile.
	impostor := likeTestProfileFor("owner", likeTestOtherUUID)

	t.Run("cannot edit", func(t *testing.T) {
		db := likeTestDB(t)
		entry := newLikeTestEntry()
		entry.Comments = []*pb.Comment{ownerComment()}

		entry, err := func() (*pb.Entry, error) {
			_, e, err := Comment(db, impostor, entry, editBy(impostor, "hijack"))
			return e, err
		}()
		if err == nil {
			t.Error("same-id different-uuid edit must be rejected")
		}
		if entry.Comments[0].Body != "original body" {
			t.Errorf("body = %q; comment must stay untouched", entry.Comments[0].Body)
		}
	})

	t.Run("cannot delete", func(t *testing.T) {
		db := likeTestDB(t)
		entry := newLikeTestEntry()
		entry.Comments = []*pb.Comment{ownerComment()}

		entry, err := DeleteComment(db, impostor, entry, likeTestCommentID)
		if err == nil {
			t.Error("same-id different-uuid delete must be rejected")
		}
		if len(entry.Comments) != 1 {
			t.Errorf("comments = %d; want 1", len(entry.Comments))
		}
	})

	t.Run("cannot unlike", func(t *testing.T) {
		db := likeTestDB(t)
		entry := newLikeTestEntry()
		entry.Likes = []*pb.Like{ownerLike()}

		entry, err := DeleteLike(db, impostor, entry)
		if err != nil {
			t.Fatalf("DeleteLike: %v", err)
		}
		if len(entry.Likes) != 1 {
			t.Errorf("likes = %d; want 1 (impostor must not remove it)", len(entry.Likes))
		}
	})

	t.Run("like is not a duplicate", func(t *testing.T) {
		db := likeTestDB(t)
		entry := newLikeTestEntry()
		entry.Likes = []*pb.Like{ownerLike()}

		_, entry, err := Like(db, impostor, entry)
		if err != nil {
			t.Fatalf("Like: %v", err)
		}
		if len(entry.Likes) != 2 {
			t.Errorf("likes = %d; want 2 (impostor's like is new, not a duplicate)", len(entry.Likes))
		}
	})
}

// UUID-less legacy records are read-only: a matching From.Id alone must
// never authorize, because the id may have been recycled. Same for
// malformed UUIDs — an unparseable UUID must not fall back to the id.
// Legacy UUID-less and malformed-UUID records stay read-only.
func TestUuidLessAndMalformedRefsNeverAuthorize(t *testing.T) {
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)

	refs := map[string]*pb.Feed{
		"empty uuid":     {Id: "owner", Name: "Owner"},
		"malformed uuid": {Uuid: "not-a-uuid", Id: "owner", Name: "Owner"},
	}
	for name, ref := range refs {
		t.Run(name+"/cannot edit", func(t *testing.T) {
			db := likeTestDB(t)
			entry := newLikeTestEntry()
			entry.Comments = []*pb.Comment{{
				Id:   likeTestCommentID,
				Body: "original body",
				From: ref,
			}}

			if _, _, err := Comment(db, owner, entry, editBy(owner, "hijack")); err == nil {
				t.Error("uuid-less/malformed ref must not authorize edit via matching id")
			}
		})

		t.Run(name+"/cannot delete", func(t *testing.T) {
			db := likeTestDB(t)
			entry := newLikeTestEntry()
			entry.Comments = []*pb.Comment{{
				Id:   likeTestCommentID,
				Body: "original body",
				From: ref,
			}}

			entry, err := DeleteComment(db, owner, entry, likeTestCommentID)
			if err == nil {
				t.Error("uuid-less/malformed ref must not authorize delete via matching id")
			}
			if len(entry.Comments) != 1 {
				t.Errorf("comments = %d; want 1", len(entry.Comments))
			}
		})

		t.Run(name+"/cannot unlike", func(t *testing.T) {
			db := likeTestDB(t)
			entry := newLikeTestEntry()
			entry.Likes = []*pb.Like{{From: ref}}

			entry, err := DeleteLike(db, owner, entry)
			if err != nil {
				t.Fatalf("DeleteLike: %v", err)
			}
			if len(entry.Likes) != 1 {
				t.Errorf("likes = %d; want 1", len(entry.Likes))
			}
		})

		t.Run(name+"/like is not a duplicate", func(t *testing.T) {
			db := likeTestDB(t)
			entry := newLikeTestEntry()
			entry.Likes = []*pb.Like{{From: ref}}

			_, entry, err := Like(db, owner, entry)
			if err != nil {
				t.Fatalf("Like: %v", err)
			}
			if len(entry.Likes) != 2 {
				t.Errorf("likes = %d; want 2 (malformed ref must not dedupe against the caller)", len(entry.Likes))
			}
		})
	}
}

// nil From is part of the no-fallback contract: panic-safety alone is
// not enough, a nil identity must never be treated as the caller.
// A nil identity is not the caller (panics and unauthorized mutations
// are both rejected).
func TestNilFromNeverAuthorizes(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)
	commentID := uuid.Must(uuid.NewV4()).String()

	t.Run("cannot edit", func(t *testing.T) {
		entry := newLikeTestEntry()
		entry.Comments = []*pb.Comment{{Id: commentID, Body: "original body", From: nil}}

		var err error
		mustNotPanic(t, "Comment", func() {
			_, _, err = Comment(db, owner, entry, &pb.Comment{
				Id:   commentID,
				Body: "hijack",
				From: &pb.Feed{Uuid: owner.Uuid, Id: owner.Id},
			})
		})
		if err == nil {
			t.Error("nil From must not authorize edit")
		}
	})

	t.Run("cannot delete", func(t *testing.T) {
		entry := newLikeTestEntry()
		entry.Comments = []*pb.Comment{{Id: commentID, Body: "original body", From: nil}}

		var err error
		mustNotPanic(t, "DeleteComment", func() {
			entry, err = DeleteComment(db, owner, entry, commentID)
		})
		if err == nil {
			t.Error("nil From must not authorize delete")
		}
		if len(entry.Comments) != 1 {
			t.Errorf("comments = %d; want 1", len(entry.Comments))
		}
	})

	t.Run("cannot unlike", func(t *testing.T) {
		entry := newLikeTestEntry()
		entry.Likes = []*pb.Like{{From: nil}}

		mustNotPanic(t, "DeleteLike", func() {
			entry, _ = DeleteLike(db, owner, entry)
		})
		if len(entry.Likes) != 1 {
			t.Errorf("likes = %d; want 1 (nil From must not match the caller)", len(entry.Likes))
		}
	})

	t.Run("like is not a duplicate", func(t *testing.T) {
		entry := newLikeTestEntry()
		entry.Likes = []*pb.Like{{From: nil}}

		mustNotPanic(t, "Like", func() {
			_, entry, _ = Like(db, owner, entry)
		})
		if len(entry.Likes) != 2 {
			t.Errorf("likes = %d; want 2 (nil From must not dedupe against the caller)", len(entry.Likes))
		}
	})
}

// The mutation paths must tolerate nil From plus empty and malformed
// UUIDs without dereferencing them. Table-driven so one panic cannot mask later inputs,
// and every mutation path sees each malformed ref — including Comment
// update, which only compares From when the comment id matches.
func TestMalformedActorRefsDoNotPanic(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)

	commentID := uuid.Must(uuid.NewV4()).String()
	refs := []struct {
		name string
		ref  *pb.Feed
	}{
		{"nil from", nil},
		{"empty uuid", &pb.Feed{Id: "owner"}},
		{"malformed uuid", &pb.Feed{Uuid: "not-a-uuid", Id: "owner"}},
	}

	for _, tc := range refs {
		t.Run(tc.name+"/Like", func(t *testing.T) {
			entry := newLikeTestEntry()
			entry.Likes = []*pb.Like{{From: tc.ref}}
			mustNotPanic(t, "Like", func() { _, _, _ = Like(db, owner, entry) })
		})
		t.Run(tc.name+"/DeleteLike", func(t *testing.T) {
			entry := newLikeTestEntry()
			entry.Likes = []*pb.Like{{From: tc.ref}}
			mustNotPanic(t, "DeleteLike", func() { _, _ = DeleteLike(db, owner, entry) })
		})
		t.Run(tc.name+"/Comment update", func(t *testing.T) {
			entry := newLikeTestEntry()
			entry.Comments = []*pb.Comment{{Id: commentID, From: tc.ref}}
			mustNotPanic(t, "Comment", func() {
				_, _, _ = Comment(db, owner, entry, &pb.Comment{
					Id:   commentID,
					Body: "edited",
					From: &pb.Feed{Uuid: owner.Uuid, Id: owner.Id},
				})
			})
		})
		t.Run(tc.name+"/DeleteComment", func(t *testing.T) {
			entry := newLikeTestEntry()
			entry.Comments = []*pb.Comment{{Id: commentID, From: tc.ref}}
			mustNotPanic(t, "DeleteComment", func() {
				_, _ = DeleteComment(db, owner, entry, commentID)
			})
		})
	}
}

// Comment edit updates only the body: the stored author, date and id
// are preserved even when the client tries to overwrite them.
func TestCommentEditPreservesAuthorDateAndId(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)

	stored := ownerComment()
	stored.Date = "2012-09-07T07:40:22Z"
	entry := newLikeTestEntry()
	entry.Comments = []*pb.Comment{stored}

	forged := editBy(owner, "edited body")
	forged.Date = "2030-01-01T00:00:00Z" // client tries to rewrite history
	_, entry, err := Comment(db, owner, entry, forged)
	if err != nil {
		t.Fatalf("owner edit: %v", err)
	}
	got := entry.Comments[0]
	if got.Body != "edited body" {
		t.Errorf("Body = %q; want edited", got.Body)
	}
	if got.Date != "2012-09-07T07:40:22Z" {
		t.Errorf("Date = %q; want original preserved", got.Date)
	}
	if got.Id != likeTestCommentID {
		t.Errorf("Id = %q; want original preserved", got.Id)
	}
	if got.From.Id != "owner" || got.From.Uuid != owner.Uuid {
		t.Errorf("From = <%q, %q>; want original author preserved", got.From.Id, got.From.Uuid)
	}
}

// Moderation comes from entry.ProfileUuid only: an entry without it
// grants nothing through the recyclable entry.From.Id snapshot.
func TestDeleteCommentNoModerationViaEntryFromId(t *testing.T) {
	db := likeTestDB(t)
	entryAuthor := likeTestProfileFor("entry", likeTestEntryUUID)

	entry := newLikeTestEntry()
	entry.ProfileUuid = "" // legacy entry without a stable author id
	entry.From = &pb.Feed{Id: "entry"}
	entry.Comments = []*pb.Comment{ownerComment()}

	entry, err := DeleteComment(db, entryAuthor, entry, likeTestCommentID)
	if err == nil {
		t.Error("entry.From.Id must not grant moderation")
	}
	if len(entry.Comments) != 1 {
		t.Errorf("comments = %d; want 1", len(entry.Comments))
	}
}

// Deleting a non-existent comment keeps the current blind-delete
// semantics: no error, comments untouched.
func TestDeleteCommentBlindDeleteKeepsSemantics(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)

	entry := newLikeTestEntry()
	entry.Comments = []*pb.Comment{ownerComment()}

	entry, err := DeleteComment(db, owner, entry, uuid.Must(uuid.NewV4()).String())
	if err != nil {
		t.Fatalf("blind delete: %v", err)
	}
	if len(entry.Comments) != 1 {
		t.Errorf("comments = %d; want 1", len(entry.Comments))
	}
}
