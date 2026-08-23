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

const notificationFeedServiceFailedTaskType = "notification.feed_service_failed"

type feedServiceFailedNotificationPayload struct {
	Version     uint32 `json:"version"`
	ServiceUUID string `json:"service_uuid"`
	DeadAtMS    int64  `json:"dead_at_ms"`
}

func feedServiceFailureOccurrence(service, target uuid.UUID, deadAtMS int64) string {
	return fmt.Sprintf("%s:%s:%d", service, target, deadAtMS)
}

func feedServiceFailedNotificationTaskDefinition(handler taskqueue.Handler) taskqueue.Definition {
	return taskqueue.Definition{
		ValidatePayload: func(payload []byte, version uint32) error {
			if version != 1 {
				return fmt.Errorf("unsupported payload version %d", version)
			}
			var message feedServiceFailedNotificationPayload
			if err := json.Unmarshal(payload, &message); err != nil {
				return err
			}
			if message.Version != 1 || message.DeadAtMS <= 0 {
				return errors.New("invalid FeedService failure notification payload")
			}
			serviceID, err := uuid.FromString(message.ServiceUUID)
			if err != nil || serviceID == uuid.Nil {
				return errors.New("valid service_uuid is required")
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

func feedServiceFailedNotificationSpec(serviceID uuid.UUID, deadAtMS int64) (taskqueue.Spec, error) {
	if serviceID == uuid.Nil || deadAtMS <= 0 {
		return taskqueue.Spec{}, errors.New("valid service UUID and dead time are required")
	}
	payload, err := json.Marshal(feedServiceFailedNotificationPayload{
		Version:     1,
		ServiceUUID: serviceID.String(),
		DeadAtMS:    deadAtMS,
	})
	if err != nil {
		return taskqueue.Spec{}, err
	}
	return taskqueue.Spec{
		Type:           notificationFeedServiceFailedTaskType,
		Payload:        payload,
		PayloadVersion: 1,
		IdempotencyKey: fmt.Sprintf("feed-service-failed:%s:%d", serviceID, deadAtMS),
	}, nil
}

// persistDeadServiceFailure makes the terminal ServiceState transition and
// its notification task one atomic durability boundary. A crash can therefore
// leave neither side committed or both sides committed, never a permanently
// dead source with no durable notification work.
func (s *ApiServer) persistDeadServiceFailure(ctx context.Context, serviceID uuid.UUID, state *pb.ServiceState) error {
	if state == nil || state.Status != model.ServiceStatusDead || state.DeadAtMs <= 0 {
		return errors.New("dead ServiceState with failure occurrence is required")
	}
	if err := s.ensureNotificationTaskDefinition(); err != nil {
		return err
	}
	spec, err := feedServiceFailedNotificationSpec(serviceID, state.DeadAtMs)
	if err != nil {
		return err
	}
	_, err = s.tasks.EnqueueWith(ctx, []taskqueue.Spec{spec}, func(batch *pebble.Batch) error {
		return model.StagePutServiceState(batch, serviceID, state)
	})
	return err
}

// handleFeedServiceFailedNotificationTask deliberately processes the complete
// binding/admin set in one task. In this deployment a Service has fewer than
// roughly ten bindings and a Group normally has only a handful of admins, so
// pagination/continuation tasks would add more state and failure modes than
// they save. Each target commits independently: if one recipient administers
// several affected Groups, NotificationState counters remain correct, and a
// later failure is safely retried through deterministic notification IDs.
func (s *ApiServer) handleFeedServiceFailedNotificationTask(ctx context.Context, task *pb.Task) error {
	var payload feedServiceFailedNotificationPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return err
	}
	serviceID, err := uuid.FromString(payload.ServiceUUID)
	if err != nil || serviceID == uuid.Nil || payload.DeadAtMS <= 0 {
		return errors.New("invalid FeedService failure notification task")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	bindings, err := model.ListServiceFeedBindings(s.rdb, serviceID)
	if err != nil {
		return err
	}
	activity := time.UnixMilli(payload.DeadAtMS).UTC()
	seenTargets := make(map[uuid.UUID]struct{}, len(bindings))

	for _, ref := range bindings {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, seen := seenTargets[ref.TargetFeedUUID]; seen {
			continue
		}

		binding, loadErr := model.GetFeedService(s.rdb, ref.TargetFeedUUID, ref.ServiceID)
		if errors.Is(loadErr, model.ErrNotFound) {
			continue
		}
		if loadErr != nil {
			return loadErr
		}
		if !binding.Enabled || binding.ServiceUuid != serviceID.String() {
			continue
		}
		// A binding created after this failure cycle did not experience
		// the outage and should not inherit its historical notification.
		if binding.Created > 0 && binding.Created > activity.Unix() {
			continue
		}

		target, targetErr := model.GetProfileFromUuid(s.rdb, ref.TargetFeedUUID)
		if errors.Is(targetErr, model.ErrNotFound) || errors.Is(targetErr, model.ErrProfileDeleted) {
			continue
		}
		if targetErr != nil {
			return targetErr
		}

		createdAt := time.Now().UTC()
		results := make([]notificationStageResult, 0)
		switch target.Type {
		case "user":
			err = s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
				result, stageErr := s.stageNotification(
					batch,
					model.NotificationFeedServiceFailed,
					feedServiceFailureOccurrence(serviceID, ref.TargetFeedUUID, payload.DeadAtMS),
					ref.TargetFeedUUID,
					uuid.Nil,
					ref.TargetFeedUUID,
					uuid.Nil,
					uuid.Nil,
					"",
					activity,
					createdAt,
				)
				if stageErr == nil {
					results = append(results, result)
				}
				return stageErr
			})

		case "group":
			var admins []uuid.UUID
			admins, err = model.ListGroupAdmins(s.rdb, ref.TargetFeedUUID)
			if err != nil {
				return err
			}
			results = make([]notificationStageResult, 0, len(admins))
			err = s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
				for _, recipient := range admins {
					result, stageErr := s.stageNotification(
						batch,
						model.NotificationFeedServiceFailed,
						feedServiceFailureOccurrence(serviceID, ref.TargetFeedUUID, payload.DeadAtMS),
						recipient,
						uuid.Nil,
						ref.TargetFeedUUID,
						uuid.Nil,
						uuid.Nil,
						"",
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

		default:
			seenTargets[ref.TargetFeedUUID] = struct{}{}
			continue
		}
		if err != nil {
			return err
		}
		for _, result := range results {
			s.finishNotificationStage(result)
		}
		seenTargets[ref.TargetFeedUUID] = struct{}{}
	}
	return nil
}
