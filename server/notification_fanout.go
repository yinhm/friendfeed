package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
)

const (
	notificationGroupFollowRequestTaskType = "notification.follow_request_group"
	notificationFanoutPageSize             = 100
)

type groupFollowRequestNotificationPayload struct {
	Version       uint32 `json:"version"`
	FeedUUID      string `json:"feed_uuid"`
	RequesterUUID string `json:"requester_uuid"`
	RequestedAt   string `json:"requested_at"`
	ActivityAtMS  int64  `json:"activity_at_ms"`
	Cursor        string `json:"cursor,omitempty"`
}

func followRequestOccurrence(feed, requester uuid.UUID, requestedAt string) string {
	return fmt.Sprintf("%s:%s:%s", feed, requester, requestedAt)
}

func notificationFanoutTaskDefinition(handler taskqueue.Handler) taskqueue.Definition {
	return taskqueue.Definition{
		ValidatePayload: func(payload []byte, version uint32) error {
			if version != 1 {
				return fmt.Errorf("unsupported payload version %d", version)
			}
			var message groupFollowRequestNotificationPayload
			if err := json.Unmarshal(payload, &message); err != nil {
				return err
			}
			if message.Version != 1 || message.RequestedAt == "" || message.ActivityAtMS < 0 {
				return errors.New("invalid Group follow-request notification payload")
			}
			feed, feedErr := uuid.FromString(message.FeedUUID)
			requester, requesterErr := uuid.FromString(message.RequesterUUID)
			if feedErr != nil || requesterErr != nil || feed == uuid.Nil || requester == uuid.Nil {
				return errors.New("valid feed_uuid and requester_uuid are required")
			}
			if message.Cursor != "" {
				cursor, err := uuid.FromString(message.Cursor)
				if err != nil || cursor == uuid.Nil {
					return errors.New("invalid admin cursor")
				}
			}
			return nil
		},
		MaxAttempts:   5,
		LeaseDuration: 2 * time.Minute,
		MaxLease:      30 * time.Minute,
		BackoffBase:   time.Minute,
		BackoffCap:    30 * time.Minute,
		Handler:       handler,
	}
}

// ensureNotificationTaskDefinition is safe to call repeatedly. Startup
// installs the definitions before workers snapshot handled types. Mutation
// paths only fill missing definitions for tests/embedded callers, avoiding
// registry writes once a normal worker pool is running.
func (s *ApiServer) ensureNotificationTaskDefinition() error {
	if s.tasks == nil {
		return taskqueue.ErrClosed
	}
	definitions := []struct {
		taskType   string
		definition taskqueue.Definition
	}{
		{
			taskType:   notificationGroupFollowRequestTaskType,
			definition: notificationFanoutTaskDefinition(s.handleGroupFollowRequestNotificationTask),
		},
		{
			taskType:   notificationFeedServiceFailedTaskType,
			definition: feedServiceFailedNotificationTaskDefinition(s.handleFeedServiceFailedNotificationTask),
		},
	}
	for _, item := range definitions {
		if _, exists := s.tasks.Definition(item.taskType); exists {
			continue
		}
		if err := s.tasks.RegisterDefinition(item.taskType, item.definition); err != nil {
			return err
		}
	}
	return nil
}

func groupFollowRequestNotificationSpec(feed, requester uuid.UUID, requestedAt string, activity time.Time, cursor string) (taskqueue.Spec, error) {
	payload := groupFollowRequestNotificationPayload{
		Version:       1,
		FeedUUID:      feed.String(),
		RequesterUUID: requester.String(),
		RequestedAt:   requestedAt,
		ActivityAtMS:  activity.UTC().UnixMilli(),
		Cursor:        cursor,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return taskqueue.Spec{}, err
	}
	return taskqueue.Spec{
		Type:           notificationGroupFollowRequestTaskType,
		Payload:        raw,
		PayloadVersion: 1,
		IdempotencyKey: fmt.Sprintf("group-follow-request:%s:%s:%s:%s", feed, requester, requestedAt, cursor),
	}, nil
}

func (s *ApiServer) handleGroupFollowRequestNotificationTask(ctx context.Context, task *pb.Task) error {
	var payload groupFollowRequestNotificationPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return err
	}
	feed, err := uuid.FromString(payload.FeedUUID)
	if err != nil || feed == uuid.Nil {
		return errors.New("invalid fanout feed UUID")
	}
	requester, err := uuid.FromString(payload.RequesterUUID)
	if err != nil || requester == uuid.Nil {
		return errors.New("invalid fanout requester UUID")
	}
	var cursor uuid.UUID
	if payload.Cursor != "" {
		cursor, err = uuid.FromString(payload.Cursor)
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	admins, next, err := model.ListGroupAdminsPage(s.rdb, feed, notificationFanoutPageSize, cursor)
	if err != nil {
		return err
	}
	activity := time.UnixMilli(payload.ActivityAtMS).UTC()
	createdAt := time.Now().UTC()
	results := make([]notificationStageResult, 0, len(admins))
	if len(admins) > 0 {
		err = s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
			for _, recipient := range admins {
				result, stageErr := s.stageNotification(
					batch,
					model.NotificationFollowRequestReceived,
					followRequestOccurrence(feed, requester, payload.RequestedAt),
					recipient,
					requester,
					feed,
					uuid.Nil,
					uuid.Nil,
					payload.RequestedAt,
					activity,
					createdAt,
				)
				if stageErr != nil {
					return stageErr
				}
				results = append(results, result)
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, result := range results {
			s.finishNotificationStage(result)
		}
	}
	if next != "" {
		spec, err := groupFollowRequestNotificationSpec(feed, requester, payload.RequestedAt, activity, next)
		if err != nil {
			return err
		}
		_, err = s.tasks.Enqueue(ctx, spec)
		return err
	}
	return nil
}
