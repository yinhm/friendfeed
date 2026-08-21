package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
)

// GraphFollow is a compatible entry point for the generic Follow/Unfollow
// mutation. When feed_uuid names a Group, it routes into the Group
// Join/Leave domain layer instead of unconditionally writing/deleting the
// two edges, so admin-leave protection and last-admin protection apply
// uniformly regardless of entry point.
func (s *ApiServer) GraphFollow(ctx context.Context, req *pb.FollowRequest) (*pb.FollowResponse, error) {
	if req == nil {
		return nil, errors.New("follow request is required")
	}
	slog.Debug("GraphFollow", "profile_uuid", req.ProfileUuid, "feed_uuid", req.FeedUuid)

	profileUUID, err := uuid.FromString(req.ProfileUuid)
	if err != nil {
		return nil, err
	}
	feedUUID, err := uuid.FromString(req.FeedUuid)
	if err != nil {
		return nil, err
	}

	target, err := model.GetProfileFromUuid(s.rdb, feedUUID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		// In particular, never fall back to generic edge writes for a
		// soft-deleted Group/Profile.
		return nil, err
	}
	isGroup := err == nil && target.Type == "group"

	followed := false
	followkey := model.NewKeyFrom(model.Follow.Prefix, profileUUID.Bytes(), feedUUID.Bytes())
	switch req.Action {
	case "follow":
		// A private target (user feed or Group) never gets a direct edge:
		// following it requires approval, so the follow becomes a pending
		// follow request instead.
		if target != nil && target.Private {
			if err := s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
				return model.StageRequestFollow(s.rdb, batch, feedUUID, profileUUID, time.Now())
			}); err != nil {
				return nil, taskRPCError(followRequestModelError(err))
			}
			return &pb.FollowResponse{Followed: false, Requested: true}, nil
		}
		if isGroup {
			// Group membership and the Home rebuild task commit in one
			// batch, per docs/group.md's timeline rule.
			spec, err := homeRebuildSpec(profileUUID)
			if err != nil {
				return nil, err
			}
			if _, err := s.tasks.EnqueueWith(ctx, []taskqueue.Spec{spec}, func(batch *pebble.Batch) error {
				return model.StageJoinGroup(s.rdb, batch, feedUUID, profileUUID)
			}); err != nil {
				return nil, err
			}
			followed = true
			break
		}
		followerkey := model.NewKeyFrom(model.Follower.Prefix, feedUUID.Bytes(), profileUUID.Bytes())
		err = s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
			if err := batch.Set(followkey, []byte("1"), nil); err != nil {
				return fmt.Errorf("write follow edge: %w", err)
			}
			if err := batch.Set(followerkey, []byte("1"), nil); err != nil {
				return fmt.Errorf("write follower edge: %w", err)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		followed = true
	case "unfollow":
		if isGroup {
			spec, err := homeRebuildSpec(profileUUID)
			if err != nil {
				return nil, err
			}
			if _, err := s.tasks.EnqueueWith(ctx, []taskqueue.Spec{spec}, func(batch *pebble.Batch) error {
				return model.StageLeaveGroup(s.rdb, batch, feedUUID, profileUUID)
			}); err != nil {
				return nil, err
			}
			followed = false
			break
		}
		followerkey := model.NewKeyFrom(model.Follower.Prefix, feedUUID.Bytes(), profileUUID.Bytes())
		err = s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
			if err := batch.Delete(followkey, nil); err != nil {
				return fmt.Errorf("delete follow edge: %w", err)
			}
			if err := batch.Delete(followerkey, nil); err != nil {
				return fmt.Errorf("delete follower edge: %w", err)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		followed = false
	default:
		// follow
		followed, err = s.rdb.Exists(followkey)
		if err != nil {
			return nil, err
		}
	}
	return &pb.FollowResponse{Followed: followed}, nil
}
