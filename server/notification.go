package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
)

type notificationPageDTO struct {
	Version    uint32                     `json:"version"`
	Items      []model.NotificationRecord `json:"items"`
	NextCursor string                     `json:"next_cursor,omitempty"`
}

type notificationSummaryDTO struct {
	Version     uint32 `json:"version"`
	UnreadCount uint32 `json:"unread_count"`
	TotalCount  uint32 `json:"total_count"`
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
	if profile.Deleted || profile.Type != "user" {
		return errors.New("notification recipient is not an active user")
	}
	return nil
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
		items, next, err := model.ListNotifications(s.rdb, recipient, 30, cursor)
		if err != nil {
			return nil, true, err
		}
		page := notificationPageDTO{Version: 1, Items: items}
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
