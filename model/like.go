package model

import (
	"errors"
	"slices"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

// errCommentPerm rejects comment edits/deletes outside the stable-UUID
// ownership and moderation rules.
var errCommentPerm = errors.New("403: perm error")

// returns a full key and entry if succedd
func Like(db *store.Store, profile *pb.Profile, entry *pb.Entry) (store.Key, *pb.Entry, error) {
	// Validate the caller's identity before anything else: the canonical
	// mint must not be bypassed by a dedupe hit, and a nil profile must
	// not panic the dedupe scan.
	from, err := feedFromProfile(profile)
	if err != nil {
		return nil, nil, err
	}

	var key store.Key
	index := slices.IndexFunc(entry.Likes, func(like *pb.Like) bool {
		return permOwnedBy(like.From, profile)
	})
	if index == -1 {
		like := &pb.Like{
			Date: time.Now().Format(time.RFC3339),
			From: from,
		}
		entry.Likes = append(entry.Likes, like)
		key, err = PutEntry(db, entry)
	}
	return key, entry, err
}

func DeleteLike(db *store.Store, profile *pb.Profile, entry *pb.Entry) (*pb.Entry, error) {
	var err error
	if _, err = feedFromProfile(profile); err != nil {
		return nil, err
	}
	index := slices.IndexFunc(entry.Likes, func(like *pb.Like) bool {
		return permOwnedBy(like.From, profile)
	})
	if index >= 0 {
		entry.Likes = append(entry.Likes[:index], entry.Likes[index+1:]...)
		_, err = PutEntry(db, entry)
	}
	return entry, err
}

func Comment(db *store.Store, profile *pb.Profile, entry *pb.Entry, comment *pb.Comment) (store.Key, *pb.Entry, error) {
	// Validate the caller's identity before scanning existing comments;
	// the author reference always comes from the canonical profile
	// resolved server-side, any From the caller sent is display data.
	from, err := feedFromProfile(profile)
	if err != nil {
		return nil, nil, err
	}

	// is update?
	idx := -1
	for i, cmt := range entry.Comments {
		if cmt == nil || cmt.Id != comment.Id {
			continue
		}
		// Only the comment author may edit, verified by stable UUID.
		if !permOwnedBy(cmt.From, profile) {
			return nil, entry, errCommentPerm
		}
		idx = i
		break
	}

	if idx >= 0 {
		// Edit in place: only the body is editable; author, date, id and
		// every other stored field are preserved from client overwrites.
		stored := entry.Comments[idx]
		stored.Body = comment.Body
		stored.RawBody = comment.RawBody
	} else {
		comment.From = from
		entry.Comments = append(entry.Comments, comment)
	}
	key, err := PutEntry(db, entry)
	return key, entry, err
}

func DeleteComment(db *store.Store, profile *pb.Profile, entry *pb.Entry, commentId string) (*pb.Entry, error) {
	if _, err := feedFromProfile(profile); err != nil {
		return nil, err
	}
	index := slices.IndexFunc(entry.Comments, func(cmt *pb.Comment) bool {
		return cmt != nil && cmt.Id == commentId
	})
	if index < 0 {
		return entry, nil // blind delete, keep current semantics
	}
	if !canModerateComment(profile, entry, entry.Comments[index]) {
		return entry, errCommentPerm
	}
	entry.Comments = append(entry.Comments[:index], entry.Comments[index+1:]...)
	_, err := PutEntry(db, entry)
	return entry, err
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
