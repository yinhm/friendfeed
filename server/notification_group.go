package server

import (
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
)

func groupTransitionOccurrence(group, target uuid.UUID, activity time.Time) string {
	return fmt.Sprintf("%s:%s:%d", group, target, activity.UTC().UnixNano())
}

func (s *ApiServer) stageGroupTransitionNotification(batch *pebble.Batch, kind model.NotificationKind,
	actor, group, target uuid.UUID, activity time.Time) (notificationStageResult, error) {
	return s.stageNotification(
		batch,
		kind,
		groupTransitionOccurrence(group, target, activity),
		target,
		actor,
		group,
		uuid.Nil,
		uuid.Nil,
		"",
		activity,
		activity,
	)
}
