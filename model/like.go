package model

import (
	"errors"
	"slices"
	"time"

	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

// returns a full key and entry if succedd
func Like(db *store.Store, profile *pb.Profile, entry *pb.Entry) (store.Key, *pb.Entry, error) {
	var err error
	var key store.Key
	index := slices.IndexFunc(entry.Likes, func(like *pb.Like) bool {
		return like.From.Id == profile.Id
	})
	if index == -1 {
		from, err := feedFromProfile(profile)
		if err != nil {
			return nil, nil, err
		}
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
	index := slices.IndexFunc(entry.Likes, func(like *pb.Like) bool {
		return like.From.Id == profile.Id
	})
	if index >= 0 {
		entry.Likes = append(entry.Likes[:index], entry.Likes[index+1:]...)
		_, err = PutEntry(db, entry)
	}
	return entry, err
}

func Comment(db *store.Store, profile *pb.Profile, entry *pb.Entry, comment *pb.Comment) (store.Key, *pb.Entry, error) {
	var err error

	// is update?
	idx := -1
	for i, cmt := range entry.Comments {
		if cmt.Id == comment.Id {
			// recheck perm
			if cmt.From.Id != comment.From.Id {
				return nil, nil, errors.New("403: perm error")
			}
			idx = i
			break
		}
	}

	// The author identity always comes from the canonical profile resolved
	// server-side; any From the caller sent is display data at best.
	from, err := feedFromProfile(profile)
	if err != nil {
		return nil, nil, err
	}
	comment.From = from

	if idx >= 0 {
		entry.Comments[idx] = comment
	} else {
		entry.Comments = append(entry.Comments, comment)
	}
	key, err := PutEntry(db, entry)
	return key, entry, err
}

func DeleteComment(db *store.Store, profile *pb.Profile, entry *pb.Entry, commentId string) (*pb.Entry, error) {
	var err error
	index := slices.IndexFunc(entry.Comments, func(comment *pb.Comment) bool {
		return comment.Id == commentId
	})
	if index >= 0 {
		entry.Comments = append(entry.Comments[:index], entry.Comments[index+1:]...)
		_, err = PutEntry(db, entry)
	}
	return entry, err
}
