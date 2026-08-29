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
	"github.com/yinhm/friendfeed/store"
	taskqueue "github.com/yinhm/friendfeed/task"
)

const feedArchiveRebuildTaskType = "feed.archive.rebuild"
const feedArchiveRebuildAfter = 7 * 24 * time.Hour

func feedArchiveTaskDefinition(handler taskqueue.Handler) taskqueue.Definition {
	return taskqueue.Definition{
		ValidatePayload: func(payload []byte, version uint32) error {
			if version != 1 {
				return fmt.Errorf("unsupported payload version %d", version)
			}
			feed, err := uuid.FromBytes(payload)
			if err != nil || feed == uuid.Nil {
				return errors.New("payload must be one raw Feed UUID")
			}
			return nil
		},
		MaxPayloadBytes: uuid.Size,
		MaxAttempts:     3,
		LeaseDuration:   2 * time.Minute,
		MaxLease:        30 * time.Minute,
		BackoffBase:     time.Minute,
		BackoffCap:      30 * time.Minute,
		Handler:         handler,
	}
}

func (s *ApiServer) handleFeedArchiveTask(ctx context.Context, task *pb.Task) error {
	feed, err := uuid.FromBytes(task.Payload)
	if err != nil || feed == uuid.Nil {
		return errors.New("payload must be one raw Feed UUID")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	profile, err := model.GetProfileFromUuid(s.rdb, feed)
	if errors.Is(err, model.ErrNotFound) || errors.Is(err, model.ErrProfileDeleted) {
		return nil
	}
	if err != nil {
		return err
	}
	if profile.Type != "user" && profile.Type != "group" {
		return nil
	}

	// Serialize the scan-and-publish window against Entry creation/deletion.
	// Otherwise a concurrent mutation could invalidate the old snapshot before
	// this task publishes a newly stale replacement.
	s.entryLifecycleMu.RLock()
	defer s.entryLifecycleMu.RUnlock()
	stats, err := model.BuildFeedArchive(s.rdb, feed)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return model.PutFeedArchive(s.rdb, feed, stats)
}

func (s *ApiServer) enqueueFeedArchiveRebuild(ctx context.Context, feed uuid.UUID) {
	_, err := s.tasks.Enqueue(ctx, taskqueue.Spec{
		Type: feedArchiveRebuildTaskType, Payload: feed.Bytes(), PayloadVersion: 1,
		IdempotencyKey: feed.String(),
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, taskqueue.ErrClosed) {
		slog.Warn("enqueue Feed archive rebuild", "feed", feed, "error", err)
	}
}

// attachFeedArchive serves and maintains archive snapshots only for
// authenticated readers. A stale snapshot remains readable; only a dirty
// marker at least one week old stages rebuild.
func (s *ApiServer) attachFeedArchive(ctx context.Context, viewerRaw string, feed uuid.UUID, response *pb.Feed) {
	if viewerRaw == "" || feed == uuid.Nil || response == nil {
		return
	}
	stats, err := model.GetFeedArchive(s.rdb, feed)
	if err == nil {
		response.Archive = stats
	} else {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Warn("read Feed archive", "feed", feed, "error", err)
		}
		// A missing or invalid snapshot has nothing useful to serve and does
		// not benefit from the one-week stale window.
		s.enqueueFeedArchiveRebuild(ctx, feed)
		return
	}
	dirtySince, err := model.FeedArchiveDirtySince(s.rdb, feed)
	if errors.Is(err, store.ErrNotFound) {
		return
	}
	if err != nil {
		slog.Warn("read Feed archive dirty marker", "feed", feed, "error", err)
		// A malformed derived marker must not permanently prevent repair.
		s.enqueueFeedArchiveRebuild(ctx, feed)
		return
	}
	if time.Since(dirtySince) >= feedArchiveRebuildAfter {
		s.enqueueFeedArchiveRebuild(ctx, feed)
	}
}
