package model

import (
	"fmt"
	"time"

	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

// returns a full key and entry if succedd
func Like(db *store.Store, profile *pb.Profile, entry *pb.Entry) (store.Key, *pb.Entry, error) {
	var err error
	var key store.Key
	index := -1
	for i, like := range entry.Likes {
		if like.From.Id == profile.Id {
			index = i
			break
		}
	}
	if index == -1 {
		like := &pb.Like{
			Date: time.Now().Format(time.RFC3339),
			From: &pb.Feed{
				Id:   profile.Id,
				Name: profile.Name,
				Type: profile.Type,
			},
		}
		entry.Likes = append(entry.Likes, like)
		key, err = PutEntry(db, entry)
	}
	return key, entry, err
}

func DeleteLike(db *store.Store, profile *pb.Profile, entry *pb.Entry) (*pb.Entry, error) {
	var err error
	index := -1
	for i, like := range entry.Likes {
		if like.From.Id == profile.Id {
			index = i
			break
		}
	}
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
				return nil, nil, fmt.Errorf("403: perm error")
			}
			idx = i
			break
		}
	}
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
	index := -1
	for i, cmt := range entry.Comments {
		if commentId == cmt.Id {
			index = i
			break
		}
	}
	if index >= 0 {
		entry.Comments = append(entry.Comments[:index], entry.Comments[index+1:]...)
		_, err = PutEntry(db, entry)
	}
	return entry, err
}
