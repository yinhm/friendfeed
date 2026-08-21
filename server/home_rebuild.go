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

// homeRebuildTaskType is retained for persisted task compatibility. New
// payloads incrementally add/remove one Follow feed; viewer-only legacy
// payloads rebuild the bounded Home from all current edges.
const homeRebuildTaskType = "home.rebuild"

const (
	homeFeedActionAdd    = "add"
	homeFeedActionRemove = "remove"
	homeFeedAddLimit     = 100
)

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
			// viewer-only payloads were emitted before relationship maintenance
			// became incremental; keep consuming them as one full rebuild.
			if message.FeedUuid == "" && message.Action == "" && message.Limit == 0 {
				return nil
			}
			feed, err := uuid.FromString(message.FeedUuid)
			if err != nil || feed == uuid.Nil {
				return errors.New("valid feed_uuid is required")
			}
			switch message.Action {
			case homeFeedActionAdd:
				if message.Limit == 0 || message.Limit > homeFeedAddLimit {
					return fmt.Errorf("add limit must be between 1 and %d", homeFeedAddLimit)
				}
			case homeFeedActionRemove:
				if message.Limit != 0 {
					return errors.New("remove limit must be zero")
				}
			default:
				return errors.New("action must be add or remove")
			}
			return nil
		},
		MaxAttempts: 3, LeaseDuration: 2 * time.Minute, MaxLease: 30 * time.Minute,
		BackoffBase: time.Minute, BackoffCap: 30 * time.Minute, Handler: handler,
	}
}

// newHomeFeedTask builds one relationship-maintenance task. No idempotency
// key: each task rechecks the current edge, so rapid opposite changes converge
// without collapsing a legitimate transition.
func newHomeFeedTask(viewer, feed uuid.UUID, action string) (taskqueue.Spec, error) {
	message := &pb.HomeRebuildPayload{
		ViewerUuid: viewer.String(),
		FeedUuid:   feed.String(),
		Action:     action,
	}
	if action == homeFeedActionAdd {
		message.Limit = homeFeedAddLimit
	}
	payload, err := proto.Marshal(message)
	if err != nil {
		return taskqueue.Spec{}, err
	}
	return taskqueue.Spec{
		Type: homeRebuildTaskType, Payload: payload, PayloadVersion: 1,
	}, nil
}

// handleHomeRebuildTask incrementally maintains one relationship. Legacy
// viewer-only payloads still perform a full rebuild once.
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
	if payload.FeedUuid == "" {
		return s.rebuildHomeTimelineNow(viewer, s.rssNow().UTC())
	}
	feed, err := uuid.FromString(payload.FeedUuid)
	if err != nil || feed == uuid.Nil {
		return errors.New("valid feed_uuid is required")
	}
	following, err := model.IsFollower(s.rdb, feed, viewer)
	if err != nil {
		return err
	}
	switch payload.Action {
	case homeFeedActionAdd:
		if !following {
			return nil
		}
		now := s.rssNow().UTC()
		added, skipped, err := model.MergeHomeFeed(s.rdb, viewer, feed, int(payload.Limit), model.TimelineRetentionMax, now)
		if err == nil {
			err = model.TouchTimelineState(s.rdb, viewer, now)
		}
		if err == nil {
			slog.Info("merged followed feed into home timeline", "viewer", viewer, "feed", feed, "entries", added, "skipped_dates", skipped)
		}
		return err
	case homeFeedActionRemove:
		if following {
			return nil
		}
		removed, err := model.RemoveHomeFeed(s.rdb, viewer, feed)
		if err == nil {
			slog.Info("removed unfollowed feed from home timeline", "viewer", viewer, "feed", feed, "entries", removed)
		}
		return err
	default:
		return errors.New("action must be add or remove")
	}
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
