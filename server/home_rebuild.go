package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
	"google.golang.org/protobuf/proto"
)

// homeRebuildTaskType rebuilds one viewer's bounded Home timeline from the
// current Follow edges. docs/group.md requires Join/Leave/RemoveMember to
// enqueue this idempotent task instead of only changing future fanout.
const homeRebuildTaskType = "home.rebuild"

func homeRebuildTaskDefinition(handler taskqueue.Handler) taskqueue.Definition {
	return taskqueue.Definition{
		ValidatePayload: func(payload []byte, version uint32) error {
			if version != 1 {
				return fmt.Errorf("unsupported payload version %d", version)
			}
			message := new(pb.HomeRebuildPayload)
			if err := proto.Unmarshal(payload, message); err != nil {
				return err
			}
			viewer, err := uuid.FromString(message.ViewerUuid)
			if err != nil || viewer == uuid.Nil {
				return errors.New("valid viewer_uuid is required")
			}
			return nil
		},
		MaxAttempts: 3, LeaseDuration: 2 * time.Minute, MaxLease: 30 * time.Minute,
		BackoffBase: time.Minute, BackoffCap: 30 * time.Minute, Handler: handler,
	}
}

// homeRebuildSpec builds the enqueue spec for viewer. No idempotency key:
// membership changes are rare, the handler converges from current Follow
// edges regardless, and the queue rejects reusing a completed task's key, so
// collapsing rapid changes would risk rejecting a legitimate rebuild.
func homeRebuildSpec(viewer uuid.UUID) (taskqueue.Spec, error) {
	payload, err := proto.Marshal(&pb.HomeRebuildPayload{ViewerUuid: viewer.String()})
	if err != nil {
		return taskqueue.Spec{}, err
	}
	return taskqueue.Spec{
		Type: homeRebuildTaskType, Payload: payload, PayloadVersion: 1,
	}, nil
}

// handleHomeRebuildTask rebuilds the viewer's Home unconditionally. The task
// is idempotent: rebuilds derive solely from the current Follow edges and
// bounded Home rules, so retries and collapsed duplicates converge.
func (s *ApiServer) handleHomeRebuildTask(ctx context.Context, task *pb.Task) error {
	payload := new(pb.HomeRebuildPayload)
	if err := proto.Unmarshal(task.Payload, payload); err != nil {
		return err
	}
	viewer, err := uuid.FromString(payload.ViewerUuid)
	if err != nil || viewer == uuid.Nil {
		return errors.New("valid viewer_uuid is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.rebuildHomeTimelineNow(viewer, s.rssNow().UTC())
}

// rebuildHomeTimelineNow rebuilds viewer's bounded Home from the current
// Follow edges, ignoring TimelineState. Shared by the lazy maintenance path
// and the home.rebuild task handler.
func (s *ApiServer) rebuildHomeTimelineNow(viewer uuid.UUID, now time.Time) error {
	feeds := []uuid.UUID{viewer}
	seen := map[uuid.UUID]struct{}{viewer: {}}
	followPrefix := model.NewKeyFrom(model.Follow.Prefix, viewer.Bytes())
	if _, err := s.rdb.ForwardScan(followPrefix, func(_ int, key, _ []byte) error {
		feed, err := uuid.FromBytes(key[len(followPrefix):])
		if err != nil {
			return err
		}
		if _, exists := seen[feed]; !exists {
			seen[feed] = struct{}{}
			feeds = append(feeds, feed)
		}
		return nil
	}); err != nil {
		return err
	}

	rows, skipped, err := model.BuildHomeTimeline(s.rdb, feeds, model.TimelineMaxEntries, model.TimelineRetentionMax, now)
	if err != nil {
		return err
	}
	if err := model.ReplaceHomeTimeline(s.rdb, viewer, rows); err != nil {
		return err
	}
	if err := model.TouchTimelineState(s.rdb, viewer, now); err != nil {
		return err
	}
	slog.Info("rebuilt home timeline", "viewer", viewer, "feeds", len(feeds), "entries", len(rows), "skipped_dates", skipped)
	return nil
}
