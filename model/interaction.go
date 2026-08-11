package model

import (
	"fmt"
	"sort"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func LikeDataKey(entryUUID, actorUUID uuid.UUID) store.Key {
	return NewKeyFrom(LikeData.Prefix, entryUUID.Bytes(), actorUUID.Bytes())
}

func CommentDataKey(entryUUID, commentUUID uuid.UUID) store.Key {
	return NewKeyFrom(CommentData.Prefix, entryUUID.Bytes(), commentUUID.Bytes())
}

func writeEntryInteractionsBatch(batch *pebble.Batch, entryUUID uuid.UUID, comments []*pb.Comment, likes []*pb.Like) error {
	for _, like := range likes {
		if like == nil || like.From == nil {
			return fmt.Errorf("like actor is missing")
		}
		actorUUID, err := uuid.FromString(like.From.Uuid)
		if err != nil || actorUUID == uuid.Nil {
			return fmt.Errorf("like actor UUID %q is invalid", like.From.Uuid)
		}
		raw, err := proto.Marshal(like)
		if err != nil {
			return err
		}
		if err := batch.Set(LikeDataKey(entryUUID, actorUUID), raw, nil); err != nil {
			return err
		}
	}
	for _, comment := range comments {
		if comment == nil {
			return fmt.Errorf("comment is nil")
		}
		commentUUID, err := uuid.FromString(comment.Id)
		if err != nil || commentUUID == uuid.Nil {
			return fmt.Errorf("comment UUID %q is invalid", comment.Id)
		}
		raw, err := proto.Marshal(comment)
		if err != nil {
			return err
		}
		if err := batch.Set(CommentDataKey(entryUUID, commentUUID), raw, nil); err != nil {
			return err
		}
	}
	return nil
}

func deleteEntryInteractionsBatch(db *store.Store, batch *pebble.Batch, entryUUID uuid.UUID) error {
	for _, prefix := range []store.Key{
		NewKeyFrom(LikeData.Prefix, entryUUID.Bytes()),
		NewKeyFrom(CommentData.Prefix, entryUUID.Bytes()),
	} {
		if _, err := db.ForwardScan(prefix, func(_ int, key, _ []byte) error {
			return batch.Delete(key, nil)
		}); err != nil {
			return err
		}
	}
	return nil
}

// HydrateEntryInteractions replaces legacy embedded interaction snapshots
// with the canonical independent-table rows. Values already contain their
// actor display snapshot, so hydration performs two bounded prefix scans and
// no per-actor lookups.
func HydrateEntryInteractions(db *store.Store, entry *pb.Entry) error {
	if entry == nil {
		return fmt.Errorf("entry is nil")
	}
	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return fmt.Errorf("entry UUID: %w", err)
	}

	likes := make([]*pb.Like, 0)
	likePrefix := NewKeyFrom(LikeData.Prefix, entryUUID.Bytes())
	if _, err := db.ForwardScan(likePrefix, func(_ int, _, raw []byte) error {
		like := new(pb.Like)
		if err := proto.Unmarshal(raw, like); err != nil {
			return err
		}
		likes = append(likes, like)
		return nil
	}); err != nil {
		return fmt.Errorf("load entry likes: %w", err)
	}

	comments := make([]*pb.Comment, 0)
	commentPrefix := NewKeyFrom(CommentData.Prefix, entryUUID.Bytes())
	if _, err := db.ForwardScan(commentPrefix, func(_ int, _, raw []byte) error {
		comment := new(pb.Comment)
		if err := proto.Unmarshal(raw, comment); err != nil {
			return err
		}
		comments = append(comments, comment)
		return nil
	}); err != nil {
		return fmt.Errorf("load entry comments: %w", err)
	}
	sort.Slice(comments, func(i, j int) bool {
		if comments[i].Date != comments[j].Date {
			return comments[i].Date < comments[j].Date
		}
		return comments[i].Id < comments[j].Id
	})
	entry.Likes = likes
	entry.Comments = comments
	return nil
}
