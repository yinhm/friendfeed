package model

import (
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

// stageEntryInteractionNotification stages the V1 direct notification for a
// newly-created Like or Comment. Entry.ProfileUuid is the only recipient
// source: malformed legacy authors fail closed (skip), Groups are rejected by
// StageNotification, and self interactions are skipped there as well.
func stageEntryInteractionNotification(db *store.Store, batch *pebble.Batch, kind NotificationKind,
	profile *pb.Profile, entry *pb.Entry, occurrence string, comment uuid.UUID, activity time.Time) error {
	if batch == nil || profile == nil || entry == nil {
		return nil
	}
	recipient, err := uuid.FromString(entry.ProfileUuid)
	if err != nil || recipient == uuid.Nil {
		return nil
	}
	actor, err := uuid.FromString(profile.Uuid)
	if err != nil || actor == uuid.Nil {
		return fmt.Errorf("interaction actor UUID is invalid")
	}
	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil || entryUUID == uuid.Nil {
		return fmt.Errorf("interaction entry UUID is invalid")
	}
	id, err := NotificationID(kind, occurrence, recipient)
	if err != nil {
		return err
	}
	record := NotificationRecord{
		ID:                id.String(),
		Kind:              kind,
		RecipientUUID:     recipient.String(),
		ActorUUID:         actor.String(),
		EntryUUID:         entryUUID.String(),
		ActivityAtMS:      activity.UTC().UnixMilli(),
		CreatedAtNS:       time.Now().UTC().UnixNano(),
		ActorNameSnapshot: profile.Name,
	}
	if comment != uuid.Nil {
		record.CommentUUID = comment.String()
	}
	_, _, err = StageNotification(db, batch, record)
	return err
}

func stageLikeNotification(db *store.Store, batch *pebble.Batch, profile *pb.Profile, entry *pb.Entry, activity time.Time) error {
	entryUUID, err := uuid.FromString(entry.Id)
	if err != nil {
		return err
	}
	actor, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return err
	}
	// Intentionally stable across unlike/re-like cycles to avoid toggle spam.
	occurrence := fmt.Sprintf("%s:%s", entryUUID, actor)
	return stageEntryInteractionNotification(db, batch, NotificationEntryLiked, profile, entry, occurrence, uuid.Nil, activity)
}

func stageCommentNotification(db *store.Store, batch *pebble.Batch, profile *pb.Profile, entry *pb.Entry, comment uuid.UUID, activity time.Time) error {
	return stageEntryInteractionNotification(db, batch, NotificationEntryCommented, profile, entry, comment.String(), comment, activity)
}
