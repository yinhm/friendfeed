package model

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

// Step 0 of TODO.md: target permission rules for comment/like mutations,
// written against the current implementation. Several tests are expected
// to FAIL until Step 5 lands — they are the proof of the defects:
//
//   - DeleteComment performs no ownership check at all (any user can
//     delete anyone's comment);
//   - after a profile rename, the author loses edit/unlike on their own
//     UUID-bearing (target-shape) records because comparisons key on the
//     stale From.Id snapshot;
//   - nil From references panic the mutation paths.
//
// Target rules (TODO.md Step 0):
//   - edit comment: the comment author only;
//   - delete comment: the comment author, the entry author (resolved via
//     entry.ProfileUuid), or a super admin;
//   - like: no duplicates per user; unlike removes only the caller's like.

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
	db := store.NewStore(t.TempDir())
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

// Expected to FAIL until Step 5: after a rename the stored From.Id is
// stale, and the current implementation compares ids, so the author gets
// a 403 on their own comment.
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

// Expected to FAIL until Step 5: DeleteComment currently performs no
// ownership check, so an unrelated user can delete anyone's comment.
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

// Expected to FAIL until Step 5: the stored like carries the author's
// stable UUID with a stale From.Id; dedupe by id misses it and appends.
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

// Expected to FAIL until Step 5: unlike locates the like by From.Id,
// which is stale after a rename, so the author's own like survives.
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

// Expected to FAIL (panic) until Step 5: the mutation paths dereference
// From without nil checks, and must tolerate nil From plus empty and
// malformed UUIDs.
func TestMalformedActorRefsDoNotPanic(t *testing.T) {
	db := likeTestDB(t)
	owner := likeTestProfileFor("owner", likeTestOwnerUUID)

	entry := newLikeTestEntry()
	entry.Likes = []*pb.Like{
		{From: nil},
		{From: &pb.Feed{Uuid: ""}},
		{From: &pb.Feed{Uuid: "not-a-uuid"}},
	}
	entry.Comments = []*pb.Comment{
		{Id: uuid.Must(uuid.NewV4()).String(), From: nil},
		{Id: uuid.Must(uuid.NewV4()).String(), From: &pb.Feed{Uuid: ""}},
		{Id: uuid.Must(uuid.NewV4()).String(), From: &pb.Feed{Uuid: "not-a-uuid"}},
	}

	mustNotPanic(t, "Like", func() { _, _, _ = Like(db, owner, entry) })
	mustNotPanic(t, "DeleteLike", func() { _, _ = DeleteLike(db, owner, entry) })
	mustNotPanic(t, "Comment", func() {
		_, _, _ = Comment(db, owner, entry, editBy(owner, "body"))
	})
	mustNotPanic(t, "DeleteComment", func() {
		_, _ = DeleteComment(db, owner, entry, entry.Comments[0].Id)
	})
}
