package model

import (
	"errors"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

// errCommentPerm rejects comment edits/deletes outside the stable-UUID
// ownership and moderation rules.
var errCommentPerm = errors.New("403: perm error")

// InteractionCreatedHook lets a caller stage recipient-owned side effects in
// the same Pebble batch as a newly-created Like or Comment. It is invoked only
// for the authoritative missing->present transition; retries/edits do not run
// it. The hook must only stage bounded writes and must not commit the batch.
type InteractionCreatedHook func(batch *pebble.Batch, activity time.Time) error

// returns a full key and entry if succedd
func PutLike(db *store.Store, profile *pb.Profile, entry *pb.Entry) (store.Key, *pb.Entry, error) {
	key, updated, _, err := PutLikeWithCreatedHook(db, profile, entry, nil)
	return key, updated, err
}

// PutLikeWithCreatedHook is PutLike plus a caller-supplied hook that runs in
// the same atomic batch only when the Like is first created. The bool reports
// that real transition, allowing callers to drive non-atomic derived fanout
// from the committed result instead of a racy preflight read.
func PutLikeWithCreatedHook(db *store.Store, profile *pb.Profile, entry *pb.Entry, hook InteractionCreatedHook) (store.Key, *pb.Entry, bool, error) {
	// Validate the caller's identity before anything else: the canonical
	// mint must not be bypassed by a dedupe hit, and a nil profile must
	// not panic the dedupe scan.
	from, err := feedFromProfile(profile)
	if err != nil {
		return nil, nil, false, err
	}

	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return nil, nil, false, err
	}
	actorUUID, _ := uuid.FromString(profile.Uuid)
	dataKey := LikeKey(entryUUID, actorUUID)
	created := false
	var activity time.Time
	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		if _, err := db.Get(dataKey); err == nil {
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		// The derived key has millisecond precision. Persist exactly that
		// precision so delete/rebuild can reproduce it.
		activity = time.Now().UTC().Truncate(time.Millisecond)
		like := &pb.Like{Date: activity.Format(time.RFC3339Nano), From: from}
		raw, err := proto.Marshal(like)
		if err != nil {
			return err
		}
		indexKey, err := LikeTimelineKey(actorUUID, entryUUID, activity)
		if err != nil {
			return err
		}
		if err := batch.Set(dataKey, raw, nil); err != nil {
			return err
		}
		if err := batch.Set(indexKey, nil, nil); err != nil {
			return err
		}
		if group, ok, err := entryGroupUUID(db, entry); err != nil {
			return err
		} else if ok {
			if err := stageAdjustGroupActivityIfMember(db, batch, actorUUID, group, GroupActivityLikeScore); err != nil {
				return err
			}
		}
		if hook != nil {
			if err := hook(batch, activity); err != nil {
				return err
			}
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, nil, false, err
	}
	if err := LoadEntryInteractions(db, entry); err != nil {
		return nil, nil, created, err
	}
	if created {
		if _, err := FanoutTimelineActivity(db, entry, activity, TimelineActivityLike); err != nil {
			return nil, nil, true, err
		}
	}
	return Entry.PrefixAppend(entryUUID.Bytes()), entry, created, nil
}

func DeleteLike(db *store.Store, profile *pb.Profile, entry *pb.Entry) (*pb.Entry, error) {
	if _, err := feedFromProfile(profile); err != nil {
		return nil, err
	}
	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return nil, err
	}
	actorUUID, _ := uuid.FromString(profile.Uuid)
	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		dataKey := LikeKey(entryUUID, actorUUID)
		raw, err := db.Get(dataKey)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		like := new(pb.Like)
		if err := proto.Unmarshal(raw, like); err != nil {
			return err
		}
		if err := batch.Delete(dataKey, nil); err != nil {
			return err
		}
		if group, ok, err := entryGroupUUID(db, entry); err != nil {
			return err
		} else if ok {
			if err := stageAdjustGroupActivityIfMember(db, batch, actorUUID, group, -GroupActivityLikeScore); err != nil {
				return err
			}
		}
		created, err := time.Parse(time.RFC3339, like.Date)
		if err != nil {
			// Legacy malformed Likes never had a reproducible derived key.
			// Removing the authoritative row must still remain possible.
			return nil
		}
		indexKey, err := LikeTimelineKey(actorUUID, entryUUID, created)
		if err != nil {
			return err
		}
		return batch.Delete(indexKey, nil)
	})
	if err != nil {
		return nil, err
	}
	return entry, LoadEntryInteractions(db, entry)
}

func PutComment(db *store.Store, profile *pb.Profile, entry *pb.Entry, comment *pb.Comment) (store.Key, *pb.Entry, error) {
	key, updated, _, err := PutCommentWithCreatedHook(db, profile, entry, comment, nil)
	return key, updated, err
}

// PutCommentWithCreatedHook is PutComment plus a bounded hook invoked only for
// a new comment UUID. Edits preserve the existing comment and never run the
// hook, so notification emission follows the state transition rather than RPC
// call count.
func PutCommentWithCreatedHook(db *store.Store, profile *pb.Profile, entry *pb.Entry, comment *pb.Comment, hook InteractionCreatedHook) (store.Key, *pb.Entry, bool, error) {
	// Validate the caller's identity before scanning existing comments;
	// the author reference always comes from the canonical profile
	// resolved server-side, any From the caller sent is display data.
	from, err := feedFromProfile(profile)
	if err != nil {
		return nil, nil, false, err
	}

	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return nil, nil, false, err
	}
	commentUUID, err := uuid.FromString(comment.Id)
	if err != nil {
		return nil, nil, false, err
	}
	actorUUID, _ := uuid.FromString(profile.Uuid)
	dataKey := CommentKey(entryUUID, commentUUID)
	created := false
	var activity time.Time
	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		storedRaw, getErr := db.Get(dataKey)
		if getErr == nil {
			stored := new(pb.Comment)
			if err := proto.Unmarshal(storedRaw, stored); err != nil {
				return err
			}
			if !permOwnedBy(stored.From, profile) {
				return errCommentPerm
			}
			stored.Body, stored.RawBody = comment.Body, comment.RawBody
			comment = stored
		} else if errors.Is(getErr, store.ErrNotFound) {
			comment.From = from
			// The derived key has millisecond precision. Persist exactly that
			// precision so delete/rebuild can reproduce it.
			activity = time.Now().UTC().Truncate(time.Millisecond)
			comment.Date = activity.Format(time.RFC3339Nano)
			created = true
		} else {
			return getErr
		}
		raw, err := proto.Marshal(comment)
		if err != nil {
			return err
		}
		if err := batch.Set(dataKey, raw, nil); err != nil {
			return err
		}
		if !created {
			return nil
		}
		if group, ok, err := entryGroupUUID(db, entry); err != nil {
			return err
		} else if ok {
			if err := stageAdjustGroupActivityIfMember(db, batch, actorUUID, group, GroupActivityCommentScore); err != nil {
				return err
			}
		}
		positionKey := CommentTimelinePositionKey(actorUUID, entryUUID)
		if oldRaw, err := db.Get(positionKey); err == nil {
			oldTime, oldComment, err := DecodeCommentTimelinePosition(oldRaw)
			if err != nil {
				return err
			}
			if activity.Before(oldTime) ||
				(activity.Equal(oldTime) && commentUUID.String() <= oldComment.String()) {
				if hook != nil {
					return hook(batch, activity)
				}
				return nil
			}
			oldKey, err := CommentTimelineKey(actorUUID, entryUUID, oldTime)
			if err != nil {
				return err
			}
			if err := batch.Delete(oldKey, nil); err != nil {
				return err
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		indexKey, err := CommentTimelineKey(actorUUID, entryUUID, activity)
		if err != nil {
			return err
		}
		position, err := EncodeCommentTimelinePosition(activity, commentUUID)
		if err != nil {
			return err
		}
		if err := batch.Set(indexKey, commentUUID.Bytes(), nil); err != nil {
			return err
		}
		if err := batch.Set(positionKey, position, nil); err != nil {
			return err
		}
		if hook != nil {
			if err := hook(batch, activity); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errCommentPerm) {
			return nil, entry, false, err
		}
		return nil, nil, false, err
	}
	if err := LoadEntryInteractions(db, entry); err != nil {
		return nil, nil, created, err
	}
	if created {
		if _, err := FanoutTimelineActivity(db, entry, activity, TimelineActivityComment); err != nil {
			return nil, nil, true, err
		}
	}
	return Entry.PrefixAppend(entryUUID.Bytes()), entry, created, nil
}

func DeleteComment(db *store.Store, profile *pb.Profile, entry *pb.Entry, commentId string) (*pb.Entry, error) {
	if _, err := feedFromProfile(profile); err != nil {
		return nil, err
	}
	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return nil, err
	}
	commentUUID, err := uuid.FromString(commentId)
	if err != nil {
		return nil, err
	}
	dataKey := CommentKey(entryUUID, commentUUID)
	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		raw, err := db.Get(dataKey)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		comment := new(pb.Comment)
		if err := proto.Unmarshal(raw, comment); err != nil {
			return err
		}
		if !canModerateComment(db, profile, entry, comment) {
			return errCommentPerm
		}
		actorUUID, err := uuid.FromString(comment.From.Uuid)
		if err != nil || actorUUID == uuid.Nil {
			return errors.New("comment actor UUID is invalid")
		}
		if err := batch.Delete(dataKey, nil); err != nil {
			return err
		}
		if group, ok, err := entryGroupUUID(db, entry); err != nil {
			return err
		} else if ok {
			if err := stageAdjustGroupActivityIfMember(db, batch, actorUUID, group, -GroupActivityCommentScore); err != nil {
				return err
			}
		}
		positionKey := CommentTimelinePositionKey(actorUUID, entryUUID)
		position, err := db.Get(positionKey)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		oldTime, latestID, err := DecodeCommentTimelinePosition(position)
		if err != nil {
			return err
		}
		if latestID != commentUUID {
			return nil
		}
		oldKey, err := CommentTimelineKey(actorUUID, entryUUID, oldTime)
		if err != nil {
			return err
		}
		if err := batch.Delete(oldKey, nil); err != nil {
			return err
		}
		if err := batch.Delete(positionKey, nil); err != nil {
			return err
		}
		fallback, fallbackTime, fallbackID, err := latestActorComment(db, entryUUID, actorUUID, commentUUID)
		if err != nil || fallback == nil {
			return err
		}
		newKey, err := CommentTimelineKey(actorUUID, entryUUID, fallbackTime)
		if err != nil {
			return err
		}
		newPosition, err := EncodeCommentTimelinePosition(fallbackTime, fallbackID)
		if err != nil {
			return err
		}
		if err := batch.Set(newKey, fallbackID.Bytes(), nil); err != nil {
			return err
		}
		return batch.Set(positionKey, newPosition, nil)
	})
	if err != nil {
		if errors.Is(err, errCommentPerm) {
			return entry, err
		}
		return nil, err
	}
	return entry, LoadEntryInteractions(db, entry)
}

func latestActorComment(db *store.Store, entry, actor, exclude uuid.UUID) (*pb.Comment, time.Time, uuid.UUID, error) {
	var best *pb.Comment
	var bestTime time.Time
	var bestID uuid.UUID
	prefix := NewKeyFrom(Comment.Prefix, entry.Bytes())
	_, err := db.ForwardScan(prefix, func(_ int, key, raw []byte) error {
		id, err := uuid.FromBytes(key[len(prefix):])
		if err != nil || id == exclude {
			return err
		}
		candidate := new(pb.Comment)
		if err := proto.Unmarshal(raw, candidate); err != nil {
			return err
		}
		if candidate.From == nil || candidate.From.Uuid != actor.String() {
			return nil
		}
		at, err := time.Parse(time.RFC3339, candidate.Date)
		if err != nil {
			// A malformed legacy candidate cannot define a stable sorting
			// position, but must not prevent deletion of a valid comment.
			return nil
		}
		if best == nil || at.After(bestTime) || (at.Equal(bestTime) && id.String() > bestID.String()) {
			best, bestTime, bestID = candidate, at, id
		}
		return nil
	})
	return best, bestTime, bestID, err
}

// canModerateComment reports whether profile may delete cmt: the comment
// author (stable UUID), the entry author (via entry.ProfileUuid only —
// entry.From.Id is a recyclable snapshot and grants nothing), a super admin,
// or a Group admin of entry.FeedUuid (delete-only, per docs/group.md — a
// Group admin may never edit or impersonate the comment author, so this
// branch is not consulted from PutComment's edit-in-place path).
func canModerateComment(db *store.Store, profile *pb.Profile, entry *pb.Entry, cmt *pb.Comment) bool {
	if profile.IsSuper {
		return true
	}
	if permOwnedBy(cmt.From, profile) {
		return true
	}
	entryUUID, err := uuid.FromString(entry.ProfileUuid)
	if err == nil && entryUUID != uuid.Nil {
		profileUUID, err := uuid.FromString(profile.Uuid)
		if err == nil && entryUUID == profileUUID {
			return true
		}
	}
	feedUUID, err := uuid.FromString(entry.FeedUuid)
	if err != nil || feedUUID == uuid.Nil {
		return false
	}
	feedProfile, err := GetProfileFromUuid(db, feedUUID)
	if err != nil || feedProfile.Type != "group" {
		return false
	}
	profileUUID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return false
	}
	isAdmin, err := IsGroupAdmin(db, feedUUID, profileUUID)
	if err != nil {
		return false
	}
	return isAdmin
}
