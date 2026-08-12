package model

import (
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

// errCommentPerm rejects comment edits/deletes outside the stable-UUID
// ownership and moderation rules.
var errCommentPerm = errors.New("403: perm error")

// returns a full key and entry if succedd
func PutLike(db *store.Store, profile *pb.Profile, entry *pb.Entry) (store.Key, *pb.Entry, error) {
	// Validate the caller's identity before anything else: the canonical
	// mint must not be bypassed by a dedupe hit, and a nil profile must
	// not panic the dedupe scan.
	from, err := feedFromProfile(profile)
	if err != nil {
		return nil, nil, err
	}

	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return nil, nil, err
	}
	actorUUID, _ := uuid.FromString(profile.Uuid)
	dataKey := LikeKey(entryUUID, actorUUID)
	if _, err := db.Get(dataKey); errors.Is(err, store.ErrNotFound) {
		like := &pb.Like{
			Date: time.Now().Format(time.RFC3339),
			From: from,
		}
		raw, err := proto.Marshal(like)
		if err != nil {
			return nil, nil, err
		}
		if err := db.Put(dataKey, raw); err != nil {
			return nil, nil, err
		}
	} else if err != nil {
		return nil, nil, err
	}
	if err := LoadEntryInteractions(db, entry); err != nil {
		return nil, nil, err
	}
	return Entry.PrefixAppend(entryUUID.Bytes()), entry, nil
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
	if err := db.Delete(LikeKey(entryUUID, actorUUID)); err != nil {
		return nil, err
	}
	return entry, LoadEntryInteractions(db, entry)
}

func PutComment(db *store.Store, profile *pb.Profile, entry *pb.Entry, comment *pb.Comment) (store.Key, *pb.Entry, error) {
	// Validate the caller's identity before scanning existing comments;
	// the author reference always comes from the canonical profile
	// resolved server-side, any From the caller sent is display data.
	from, err := feedFromProfile(profile)
	if err != nil {
		return nil, nil, err
	}

	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return nil, nil, err
	}
	commentUUID, err := uuid.FromString(comment.Id)
	if err != nil {
		return nil, nil, err
	}
	dataKey := CommentKey(entryUUID, commentUUID)
	storedRaw, getErr := db.Get(dataKey)
	if getErr == nil {
		stored := new(pb.Comment)
		if err := proto.Unmarshal(storedRaw, stored); err != nil {
			return nil, nil, err
		}
		// Only the comment author may edit, verified by stable UUID.
		if !permOwnedBy(stored.From, profile) {
			return nil, entry, errCommentPerm
		}
		// Edit in place: only the body is editable; author, date, id and
		// every other stored field are preserved from client overwrites.
		stored.Body = comment.Body
		stored.RawBody = comment.RawBody
		comment = stored
	} else if errors.Is(getErr, store.ErrNotFound) {
		comment.From = from
		comment.Date = time.Now().UTC().Format(time.RFC3339)
	} else {
		return nil, nil, getErr
	}
	raw, err := proto.Marshal(comment)
	if err != nil {
		return nil, nil, err
	}
	if err := db.Put(dataKey, raw); err != nil {
		return nil, nil, err
	}
	if err := LoadEntryInteractions(db, entry); err != nil {
		return nil, nil, err
	}
	return Entry.PrefixAppend(entryUUID.Bytes()), entry, nil
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
	raw, err := db.Get(dataKey)
	if errors.Is(err, store.ErrNotFound) {
		return entry, nil // blind delete, keep current semantics
	}
	if err != nil {
		return nil, err
	}
	comment := new(pb.Comment)
	if err := proto.Unmarshal(raw, comment); err != nil {
		return nil, err
	}
	if !canModerateComment(profile, entry, comment) {
		return entry, errCommentPerm
	}
	if err := db.Delete(dataKey); err != nil {
		return nil, err
	}
	return entry, LoadEntryInteractions(db, entry)
}

// canModerateComment reports whether profile may delete cmt: the comment
// author (stable UUID), the entry author (via entry.ProfileUuid only —
// entry.From.Id is a recyclable snapshot and grants nothing), or a super
// admin.
func canModerateComment(profile *pb.Profile, entry *pb.Entry, cmt *pb.Comment) bool {
	if profile.IsSuper {
		return true
	}
	if permOwnedBy(cmt.From, profile) {
		return true
	}
	entryUUID, err := uuid.FromString(entry.ProfileUuid)
	if err != nil || entryUUID == uuid.Nil {
		return false
	}
	profileUUID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return false
	}
	return entryUUID == profileUUID
}
