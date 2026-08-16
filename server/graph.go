package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

// GraphFollow is a compatible entry point for the generic Follow/Unfollow
// mutation. When feed_uuid names a Group, it routes into the Group
// Join/Leave domain layer instead of unconditionally writing/deleting the
// two edges, so admin-leave protection and last-admin protection apply
// uniformly regardless of entry point.
func (s *ApiServer) GraphFollow(ctx context.Context, req *pb.FollowRequest) (*pb.FollowResponse, error) {
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
	isGroup := err == nil && target.Type == "group"

	followed := false
	followkey := model.NewKeyFrom(model.Follow.Prefix, profileUUID.Bytes(), feedUUID.Bytes())
	switch req.Action {
	case "follow":
		if isGroup {
			if err := model.JoinGroup(s.rdb, feedUUID, profileUUID); err != nil {
				return nil, err
			}
			// Trigger Home timeline rebuild asynchronously
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				if err := model.DeleteTimelineState(s.rdb, profileUUID); err != nil {
					slog.Warn("failed to clear timeline state after GraphFollow join", "user", profileUUID, "group", feedUUID, "error", err)
				}
			}()
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
			if err := model.LeaveGroup(s.rdb, feedUUID, profileUUID); err != nil {
				return nil, err
			}
			// Trigger Home timeline rebuild asynchronously
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				if err := model.DeleteTimelineState(s.rdb, profileUUID); err != nil {
					slog.Warn("failed to clear timeline state after GraphFollow leave", "user", profileUUID, "group", feedUUID, "error", err)
				}
			}()
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
