package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type interactionRebuildStats struct{ likes, comments, indexedLikes, indexedComments, unresolvedActor, missingDate int }
type latestCommentCandidate struct {
	actor, entry, comment uuid.UUID
	at                    time.Time
}

func resolveInteractionUser(db *store.Store, raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, nil
	}
	if id, err := uuid.FromString(raw); err == nil && id != uuid.Nil {
		return id, nil
	}
	profile, err := model.GetProfileFromUserId(db, raw)
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.FromString(profile.Uuid)
}

func clearInteractionTimelines(db *store.Store, actor uuid.UUID) error {
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		prefixes := []store.Key{model.LikeTimeline.Prefix, model.CommentTimeline.Prefix, model.CommentTimelinePosition.Prefix}
		if actor != uuid.Nil {
			prefixes = []store.Key{model.LikeTimelinePrefix(actor), model.CommentTimelinePrefix(actor), model.NewKeyFrom(model.CommentTimelinePosition.Prefix, actor.Bytes())}
		}
		for _, prefix := range prefixes {
			upper := store.KeyUpperBound(prefix)
			if upper == nil {
				return fmt.Errorf("prefix %x has no upper bound", prefix)
			}
			if err := batch.DeleteRange(prefix, upper, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

func rebuildInteractionTimelines(db *store.Store, user string, dry bool) (interactionRebuildStats, error) {
	stats := interactionRebuildStats{}
	filter, err := resolveInteractionUser(db, user)
	if err != nil {
		return stats, err
	}
	if !dry {
		if err := clearInteractionTimelines(db, filter); err != nil {
			return stats, err
		}
	}
	likeWrites := make([]struct{ key store.Key }, 0, 500)
	flushLikes := func() error {
		if dry {
			stats.indexedLikes += len(likeWrites)
			likeWrites = likeWrites[:0]
			return nil
		}
		err := db.ApplyBatch(func(batch *pebble.Batch) error {
			for _, row := range likeWrites {
				if err := batch.Set(row.key, nil, nil); err != nil {
					return err
				}
			}
			return nil
		})
		stats.indexedLikes += len(likeWrites)
		likeWrites = likeWrites[:0]
		return err
	}
	err = model.Like.Iter(db, func(key, raw []byte) error {
		stats.likes++
		like := new(pb.Like)
		if err := proto.Unmarshal(raw, like); err != nil {
			return err
		}
		if like.From == nil {
			stats.unresolvedActor++
			return nil
		}
		actor, err := uuid.FromString(like.From.Uuid)
		if err != nil || actor == uuid.Nil {
			stats.unresolvedActor++
			return nil
		}
		if filter != uuid.Nil && actor != filter {
			return nil
		}
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid Like key length %d", len(key))
		}
		entry, err := uuid.FromBytes(key[4:20])
		if err != nil {
			return err
		}
		at, err := time.Parse(time.RFC3339, like.Date)
		if err != nil {
			stats.missingDate++
			return nil
		}
		index, err := model.LikeTimelineKey(actor, entry, at)
		if err != nil {
			return err
		}
		likeWrites = append(likeWrites, struct{ key store.Key }{index})
		if len(likeWrites) == cap(likeWrites) {
			return flushLikes()
		}
		return nil
	})
	if err != nil {
		return stats, err
	}
	if err := flushLikes(); err != nil {
		return stats, err
	}

	candidates := make(map[string]latestCommentCandidate, 500)
	flushComments := func() error {
		rows := make([]latestCommentCandidate, 0, len(candidates))
		for _, row := range candidates {
			rows = append(rows, row)
		}
		candidates = make(map[string]latestCommentCandidate, 500)
		if dry {
			stats.indexedComments += len(rows)
			return nil
		}
		err := db.ApplyBatch(func(batch *pebble.Batch) error {
			for _, row := range rows {
				positionKey := model.CommentTimelinePositionKey(row.actor, row.entry)
				if old, err := db.Get(positionKey); err == nil {
					oldAt, oldComment, err := model.DecodeCommentTimelinePosition(old)
					if err != nil {
						return err
					}
					if row.at.Before(oldAt) ||
						(row.at.Equal(oldAt) && row.comment.String() <= oldComment.String()) {
						continue
					}
					oldKey, _ := model.CommentTimelineKey(row.actor, row.entry, oldAt)
					if err := batch.Delete(oldKey, nil); err != nil {
						return err
					}
				} else if !errors.Is(err, store.ErrNotFound) {
					return err
				}
				index, err := model.CommentTimelineKey(row.actor, row.entry, row.at)
				if err != nil {
					return err
				}
				position, err := model.EncodeCommentTimelinePosition(row.at, row.comment)
				if err != nil {
					return err
				}
				if err := batch.Set(index, row.comment.Bytes(), nil); err != nil {
					return err
				}
				if err := batch.Set(positionKey, position, nil); err != nil {
					return err
				}
				stats.indexedComments++
			}
			return nil
		})
		return err
	}
	err = model.Comment.Iter(db, func(key, raw []byte) error {
		stats.comments++
		comment := new(pb.Comment)
		if err := proto.Unmarshal(raw, comment); err != nil {
			return err
		}
		if comment.From == nil {
			stats.unresolvedActor++
			return nil
		}
		actor, err := uuid.FromString(comment.From.Uuid)
		if err != nil || actor == uuid.Nil {
			stats.unresolvedActor++
			return nil
		}
		if filter != uuid.Nil && actor != filter {
			return nil
		}
		if len(key) != 4+2*uuid.Size {
			return fmt.Errorf("invalid Comment key length %d", len(key))
		}
		entry, _ := uuid.FromBytes(key[4:20])
		commentID, _ := uuid.FromBytes(key[20:36])
		at, err := time.Parse(time.RFC3339, comment.Date)
		if err != nil {
			stats.missingDate++
			return nil
		}
		mapKey := actor.String() + entry.String()
		current, ok := candidates[mapKey]
		if !ok || at.After(current.at) ||
			(at.Equal(current.at) && commentID.String() > current.comment.String()) {
			candidates[mapKey] = latestCommentCandidate{actor, entry, commentID, at}
		}
		if len(candidates) >= 500 {
			return flushComments()
		}
		return nil
	})
	if err != nil {
		return stats, err
	}
	if err := flushComments(); err != nil {
		return stats, err
	}
	return stats, nil
}
