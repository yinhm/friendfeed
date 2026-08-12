package main

import (
	"fmt"
	"strconv"

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
	legacyActors    int
	generatedIDs    int
	duplicates      int
}

func migrateInteractions(db *store.Store, options interactionMigrationOptions) (interactionMigrationStats, error) {
	stats, err := scanInteractions(db, options, false)
	if err != nil || options.dryRun {
		return stats, err
	}
	if stats.duplicates > 0 {
		return stats, fmt.Errorf(
			"interaction migration validation failed: duplicates=%d",
			stats.duplicates,
		)
	}
	if _, err := scanInteractions(db, options, true); err != nil {
		return stats, err
	}
	return stats, nil
}

// legacyInteractionRowUUID gives an embedded legacy interaction a stable row
// identity without pretending that it has a verified local actor identity.
// The source ordinal is stable in the stored repeated field and keeps otherwise
// identical anonymous archive records distinct.
func legacyInteractionRowUUID(entryUUID uuid.UUID, kind string, ordinal int) uuid.UUID {
	return uuid.NewV5(uuid.NamespaceURL,
		"ffdb/legacy-interaction/"+entryUUID.String()+"/"+kind+"/"+strconv.Itoa(ordinal))
}

type likeMigrationRow struct {
	id   uuid.UUID
	like *pb.Like
}

type commentMigrationRow struct {
	id      uuid.UUID
	comment *pb.Comment
}

// scanInteractions performs a complete validation pass before migrateInteractions
// starts writing. The migration is run offline, so the source data cannot change
// between validation and the subsequent bounded per-entry batches.
func scanInteractions(db *store.Store, options interactionMigrationOptions, write bool) (interactionMigrationStats, error) {
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
		entry := new(pb.Entry)
		if err := proto.Unmarshal(raw, entry); err != nil {
			return fmt.Errorf("decode Entry[%x]: %w", key, err)
		}
		if onlyProfile != uuid.Nil && entry.ProfileUuid != onlyProfile.String() {
			return nil
		}
		if options.maxLimit > 0 && stats.entriesScanned >= options.maxLimit {
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
		seenLikes := make(map[uuid.UUID]struct{})
		likeRows := make([]likeMigrationRow, 0, len(entry.Likes))
		for i, sourceLike := range entry.Likes {
			if sourceLike == nil {
				sourceLike = new(pb.Like)
			}
			like := proto.Clone(sourceLike).(*pb.Like)
			if like.From == nil {
				like.From = &pb.Feed{Name: "Unknown"}
			}
			actor := uuid.Nil
			actor, _ = uuid.FromString(like.From.Uuid)
			if actor == uuid.Nil {
				stats.legacyActors++
				actor = legacyInteractionRowUUID(entryUUID, "like", i)
				like.From.Uuid = ""
			}
			if _, exists := seenLikes[actor]; exists {
				stats.duplicates++
				valid = false
			}
			seenLikes[actor] = struct{}{}
			likeRows = append(likeRows, likeMigrationRow{id: actor, like: like})
		}
		seenComments := make(map[uuid.UUID]struct{})
		commentRows := make([]commentMigrationRow, 0, len(entry.Comments))
		for i, sourceComment := range entry.Comments {
			if sourceComment == nil {
				sourceComment = new(pb.Comment)
			}
			comment := proto.Clone(sourceComment).(*pb.Comment)
			if comment.From == nil {
				comment.From = &pb.Feed{Name: "Unknown"}
			}
			actor := uuid.Nil
			actor, _ = uuid.FromString(comment.From.Uuid)
			if actor == uuid.Nil {
				stats.legacyActors++
				comment.From.Uuid = ""
			}
			commentUUID, err := uuid.FromString(comment.Id)
			if err != nil || commentUUID == uuid.Nil {
				commentUUID = legacyInteractionRowUUID(entryUUID, "comment", i)
				comment.Id = commentUUID.String()
				stats.generatedIDs++
			}
			if _, exists := seenComments[commentUUID]; exists {
				stats.duplicates++
				valid = false
			}
			seenComments[commentUUID] = struct{}{}
			commentRows = append(commentRows, commentMigrationRow{id: commentUUID, comment: comment})
		}
		if !valid {
			return nil
		}

		stats.entriesMigrated++
		stats.likes += len(entry.Likes)
		stats.comments += len(entry.Comments)
		if !write {
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
			for _, row := range likeRows {
				raw, err := proto.Marshal(row.like)
				if err != nil {
					return err
				}
				if err := batch.Set(model.LikeKey(entryUUID, row.id), raw, nil); err != nil {
					return err
				}
			}
			for _, row := range commentRows {
				raw, err := proto.Marshal(row.comment)
				if err != nil {
					return err
				}
				if err := batch.Set(model.CommentKey(entryUUID, row.id), raw, nil); err != nil {
					return err
				}
			}
			return batch.Set(key, storedRaw, nil)
		})
	})
	return stats, err
}
