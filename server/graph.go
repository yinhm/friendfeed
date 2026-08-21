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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
		return nil, status.Error(codes.InvalidArgument, "invalid id")
	}
	feedUUID, err := uuid.FromString(req.FeedUuid)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id")
	}

	// Reuse the profile-update lock as the coarse relationship/privacy
	// mutation boundary. A target cannot flip public/private between this
	// visibility decision and the corresponding Follow/FollowRequest write.
	s.profileUpdateMu.Lock()
	defer s.profileUpdateMu.Unlock()

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
		// follow request instead. An already-approved follower is reported
		// truthfully rather than as freshly requested.
		if target != nil && target.Private {
			following, err := model.IsFollower(s.rdb, feedUUID, profileUUID)
			if err != nil {
				return nil, err
			}
			if following {
				return &pb.FollowResponse{Followed: true}, nil
			}
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
				return nil, graphFollowGroupError(err)
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
			// Self-healing: a direct follow retires any stale request for
			// the pair, so it cannot resurface on a later privacy flip.
			return model.StageDeleteFollowRequest(s.rdb, batch, feedUUID, profileUUID)
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
				return nil, graphFollowGroupError(err)
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
			// An explicit unfollow retires any stale request too: the user
			// has made their intent clear.
			return model.StageDeleteFollowRequest(s.rdb, batch, feedUUID, profileUUID)
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
		if !followed && target != nil && target.Private {
			requested, err := model.IsFollowRequestPending(s.rdb, feedUUID, profileUUID)
			if err != nil {
				return nil, err
			}
			return &pb.FollowResponse{Followed: false, Requested: requested}, nil
		}
	}
	return &pb.FollowResponse{Followed: followed}, nil
}

// graphFollowGroupError maps the Group domain layer's join/leave rejections
// onto gRPC status codes with the model message intact, so clients can show
// the user the actual reason (e.g. an admin must be demoted before leaving)
// instead of a generic internal error.
func graphFollowGroupError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, model.ErrGroupNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, model.ErrGroupAdminMustBeDemotedFirst),
		errors.Is(err, model.ErrLastGroupAdmin),
		errors.Is(err, model.ErrPrivateGroupUnsupported):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return err
	}
}
