package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
)

type notificationRecordDTO struct {
	model.NotificationRecord
	TargetType string `json:"target_type,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
}

type notificationPageDTO struct {
	Version    uint32                  `json:"version"`
	Items      []notificationRecordDTO `json:"items"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

type notificationSummaryDTO struct {
	Version     uint32 `json:"version"`
	UnreadCount uint32 `json:"unread_count"`
	TotalCount  uint32 `json:"total_count"`
}

type notificationStageResult struct {
	Recipient uuid.UUID
	Created   bool
	NeedsTrim bool
}

func parseNotificationRecipient(raw string) (uuid.UUID, error) {
	recipient, err := uuid.FromString(raw)
	if err != nil || recipient == uuid.Nil {
		return uuid.Nil, errors.New("valid notification recipient UUID is required")
	}
	return recipient, nil
}

func (s *ApiServer) validateNotificationRecipient(recipient uuid.UUID) error {
	profile, err := model.GetProfileFromUuid(s.rdb, recipient)
	if err != nil {
		return err
	}
	if profile.Type != "user" {
		return errors.New("notification recipient is not an active user")
	}
	return nil
}

func (s *ApiServer) notificationProfileName(profileID uuid.UUID) string {
	if profileID == uuid.Nil {
		return ""
	}
	profile, err := model.GetProfileFromUuid(s.rdb, profileID)
	if err != nil {
		return ""
	}
	return profile.Name
}

// notificationRecordForResponse resolves live profile presentation data
// without mutating the immutable canonical notification row. Stored snapshots
// remain the fallback when an actor/target is missing or deleted.
func (s *ApiServer) notificationRecordForResponse(record model.NotificationRecord) notificationRecordDTO {
	dto := notificationRecordDTO{NotificationRecord: record}
	if record.ActorUUID != "" {
		if actor, err := uuid.FromString(record.ActorUUID); err == nil {
			if profile, err := model.GetProfileFromUuid(s.rdb, actor); err == nil {
				dto.ActorNameSnapshot = profile.Name
			}
		}
	}
	if record.TargetUUID != "" {
		if target, err := uuid.FromString(record.TargetUUID); err == nil {
			if profile, err := model.GetProfileFromUuid(s.rdb, target); err == nil {
				dto.TargetNameSnapshot = profile.Name
				dto.TargetType = profile.Type
				dto.TargetID = profile.Id
			}
		}
	}
	return dto
}

// stageNotification writes one direct recipient notification into the
// caller's existing domain batch. It never enumerates recipients. The caller
// invokes finishNotificationStage only after the domain batch successfully
// commits so retention maintenance cannot race an uncommitted row.
func (s *ApiServer) stageNotification(batch *pebble.Batch, kind model.NotificationKind, occurrence string,
	recipient, actor, target, entry, comment uuid.UUID, requestedAt string, activity, createdAt time.Time) (notificationStageResult, error) {
	result := notificationStageResult{Recipient: recipient}
	if recipient == uuid.Nil {
		return result, nil
	}
	id, err := model.NotificationID(kind, occurrence, recipient)
	if err != nil {
		return result, err
	}
	record := model.NotificationRecord{
		ID:                 id.String(),
		Kind:               kind,
		RecipientUUID:      recipient.String(),
		RequestedAt:        requestedAt,
		ActivityAtMS:       activity.UTC().UnixMilli(),
		CreatedAtNS:        createdAt.UTC().UnixNano(),
		ActorNameSnapshot:  s.notificationProfileName(actor),
		TargetNameSnapshot: s.notificationProfileName(target),
	}
	if actor != uuid.Nil {
		record.ActorUUID = actor.String()
	}
	if target != uuid.Nil {
		record.TargetUUID = target.String()
	}
	if entry != uuid.Nil {
		record.EntryUUID = entry.String()
	}
	if comment != uuid.Nil {
		record.CommentUUID = comment.String()
	}
	created, needsTrim, err := model.StageNotification(s.rdb, batch, record)
	if err != nil {
		return result, err
	}
	result.Created = created
	result.NeedsTrim = needsTrim
	return result, nil
}

func (s *ApiServer) finishNotificationStage(result notificationStageResult) {
	if !result.Created || !result.NeedsTrim || result.Recipient == uuid.Nil {
		return
	}
	recipient := result.Recipient
	go func() {
		if !s.beginBackgroundJob() {
			return
		}
		defer s.wg.Done()
		key := "notification-trim:" + recipient.String()
		_, err, _ := s.timelineMaintenance.Do(key, func() (any, error) {
			for {
				_, remaining, err := model.TrimNotifications(s.rdb, recipient, model.NotificationTrimBatch)
				if err != nil {
					return nil, err
				}
				if !remaining {
					return nil, nil
				}
			}
		})
		if err != nil {
			slog.Error("notification trim failed", "recipient", recipient, "error", err)
		}
	}()
}

// scheduleNotificationTrimIfNeeded is used by interaction writes whose
// notification is staged inside model.PutLike/PutComment. It runs only after
// those batches have committed and converts State's threshold into the same
// lifecycle-tracked, singleflight trim used by server-owned notification
// stages.
func (s *ApiServer) scheduleNotificationTrimIfNeeded(recipient uuid.UUID) {
	if recipient == uuid.Nil {
		return
	}
	state, err := model.GetNotificationState(s.rdb, recipient)
	if err != nil || state.TotalCount <= model.NotificationTrimTrigger {
		return
	}
	s.finishNotificationStage(notificationStageResult{
		Recipient: recipient,
		Created:   true,
		NeedsTrim: true,
	})
}

// RecoverNotificationRetention streams NotificationState on startup and
// schedules bounded trims for recipients left over the cap by a crash between
// notification commit and background maintenance. It intentionally does not
// collect recipients in memory.
func (s *ApiServer) RecoverNotificationRetention() error {
	return model.NotificationState.Iter(s.rdb, func(key, _ []byte) error {
		suffix := key[len(model.NotificationState.Prefix):]
		if len(suffix) != uuid.Size {
			return nil
		}
		recipient, err := uuid.FromBytes(suffix)
		if err != nil {
			return nil
		}
		state, err := model.GetNotificationState(s.rdb, recipient)
		if err != nil {
			return err
		}
		if state.TotalCount > model.NotificationMaxEntries {
			s.finishNotificationStage(notificationStageResult{Recipient: recipient, Created: true, NeedsTrim: true})
		}
		return nil
	})
}

// notificationCommand is the narrow loopback adapter described in
// docs/notifications.md. It deliberately keeps Notification storage/domain
// logic out of httpd while avoiding a generated-protobuf migration in V1.
func (s *ApiServer) notificationCommand(ctx context.Context, cmd *pb.CommandRequest) (*pb.CommandResponse, bool, error) {
	if cmd == nil {
		return nil, false, nil
	}
	switch cmd.Command {
	case "NotificationList", "NotificationSummary", "NotificationMarkRead":
	default:
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	recipient, err := parseNotificationRecipient(cmd.Arg1)
	if err != nil {
		return nil, true, err
	}
	if err := s.validateNotificationRecipient(recipient); err != nil {
		return nil, true, err
	}

	response := &pb.CommandResponse{Command: cmd.Command}
	switch cmd.Command {
	case "NotificationList":
		var cursor []byte
		if cmd.Arg2 != "" {
			cursor, err = util.Base58Decode(cmd.Arg2)
			if err != nil || len(cursor) != model.NotificationInboxPositionSize {
				return nil, true, errors.New("invalid notification cursor")
			}
		}
		items, next, err := model.ListNotifications(s.rdb, recipient, 30, cursor)
		if err != nil {
			return nil, true, err
		}
		page := notificationPageDTO{Version: 1, Items: make([]notificationRecordDTO, 0, len(items))}
		for _, item := range items {
			page.Items = append(page.Items, s.notificationRecordForResponse(item))
		}
		if len(next) != 0 {
			page.NextCursor = util.Base58Encode(next)
		}
		raw, err := json.Marshal(page)
		if err != nil {
			return nil, true, fmt.Errorf("encode notification page: %w", err)
		}
		response.Result = string(raw)
	case "NotificationSummary":
		state, err := model.GetNotificationState(s.rdb, recipient)
		if err != nil {
			return nil, true, err
		}
		raw, err := json.Marshal(notificationSummaryDTO{
			Version:     1,
			UnreadCount: state.UnreadCount,
			TotalCount:  state.TotalCount,
		})
		if err != nil {
			return nil, true, fmt.Errorf("encode notification summary: %w", err)
		}
		response.Result = string(raw)
	case "NotificationMarkRead":
		if err := model.MarkNotificationsRead(s.rdb, recipient, time.Now().UTC()); err != nil {
			return nil, true, err
		}
		response.Result = `{"version":1}`
	}
	return response, true, nil
}
