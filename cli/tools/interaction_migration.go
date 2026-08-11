package main

import (
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type interactionMigrationOptions struct {
	user     string
	maxLimit int
	dryRun   bool
}

type interactionMigrationStats struct {
	entriesScanned  int
	entriesMigrated int
	likes           int
	comments        int
	invalidActors   int
	invalidComments int
	duplicates      int
}

func migrateInteractions(db *store.Store, options interactionMigrationOptions) (interactionMigrationStats, error) {
	stats := interactionMigrationStats{}
	var onlyProfile uuid.UUID
	if options.user != "" {
		profile, err := model.GetProfileFromUserId(db, options.user)
		if err != nil {
			return stats, fmt.Errorf("resolve profile %q: %w", options.user, err)
		}
		onlyProfile, err = uuid.FromString(profile.Uuid)
		if err != nil {
			return stats, err
		}
	}

	err := model.Entry.Iter(db, func(key, raw []byte) error {
		if options.maxLimit > 0 && stats.entriesScanned >= options.maxLimit {
			return &store.Error{Msg: "interaction migration limit reached", Code: store.StopIteration}
		}
		entry := new(pb.Entry)
		if err := proto.Unmarshal(raw, entry); err != nil {
			return fmt.Errorf("decode Entry[%x]: %w", key, err)
		}
		if onlyProfile != uuid.Nil && entry.ProfileUuid != onlyProfile.String() {
			return nil
		}
		stats.entriesScanned++
		if len(entry.Likes) == 0 && len(entry.Comments) == 0 {
			return nil
		}
		entryUUID, err := uuid.FromString(entry.Id)
		if err != nil {
			return fmt.Errorf("entry %q UUID: %w", entry.Id, err)
		}

		valid := true
		seenLikes := make(map[uuid.UUID]*pb.Like)
		for _, like := range entry.Likes {
			if like == nil || like.From == nil {
				stats.invalidActors++
				valid = false
				continue
			}
			actor, err := uuid.FromString(like.From.Uuid)
			if err != nil || actor == uuid.Nil {
				stats.invalidActors++
				valid = false
				continue
			}
			if _, exists := seenLikes[actor]; exists {
				stats.duplicates++
				valid = false
			}
			seenLikes[actor] = like
		}
		seenComments := make(map[uuid.UUID]struct{})
		for _, comment := range entry.Comments {
			if comment == nil || comment.From == nil {
				stats.invalidActors++
				valid = false
				continue
			}
			actor, actorErr := uuid.FromString(comment.From.Uuid)
			if actorErr != nil || actor == uuid.Nil {
				stats.invalidActors++
				valid = false
			}
			commentUUID, err := uuid.FromString(comment.Id)
			if err != nil || commentUUID == uuid.Nil {
				stats.invalidComments++
				valid = false
				continue
			}
			if _, exists := seenComments[commentUUID]; exists {
				stats.duplicates++
				valid = false
			}
			seenComments[commentUUID] = struct{}{}
		}
		if !valid {
			return nil
		}

		stats.entriesMigrated++
		stats.likes += len(entry.Likes)
		stats.comments += len(entry.Comments)
		if options.dryRun {
			return nil
		}
		stored := proto.Clone(entry).(*pb.Entry)
		stored.Likes = nil
		stored.Comments = nil
		storedRaw, err := proto.Marshal(stored)
		if err != nil {
			return err
		}
		return db.ApplyBatch(func(batch *pebble.Batch) error {
			for actor, like := range seenLikes {
				raw, err := proto.Marshal(like)
				if err != nil {
					return err
				}
				if err := batch.Set(model.LikeDataKey(entryUUID, actor), raw, nil); err != nil {
					return err
				}
			}
			for _, comment := range entry.Comments {
				commentUUID, _ := uuid.FromString(comment.Id)
				raw, err := proto.Marshal(comment)
				if err != nil {
					return err
				}
				if err := batch.Set(model.CommentDataKey(entryUUID, commentUUID), raw, nil); err != nil {
					return err
				}
			}
			return batch.Set(key, storedRaw, nil)
		})
	})
	return stats, err
}
