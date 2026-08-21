package server

import (
	"context"
	"errors"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
	"google.golang.org/protobuf/types/known/emptypb"
)

// authorizeFollowRequestManage checks that actor may approve, reject or list
// pending follow requests against target: the owner of a user feed (or
// super), or a Group admin/super for Groups. This is the single approver
// rule of the merged private-follow flow.
func (s *ApiServer) authorizeFollowRequestManage(actor, target uuid.UUID) error {
	targetProfile, err := model.GetProfileFromUuid(s.rdb, target)
	if err != nil {
		return err
	}
	if targetProfile.Type == "group" {
		return s.authorizeGroupManage(actor, target)
	}
	actorProfile, err := model.GetProfileFromUuid(s.rdb, actor)
	if err != nil {
		return err
	}
	if actorProfile.IsSuper || actor == target {
		return nil
	}
	return errors.New("only the feed owner may manage follow requests")
}

func parseFollowRequestIDs(actorRaw, feedRaw string) (actor, feed uuid.UUID, err error) {
	actor, actorErr := uuid.FromString(actorRaw)
	feed, feedErr := uuid.FromString(feedRaw)
	if actorErr != nil || feedErr != nil || actor == uuid.Nil || feed == uuid.Nil {
		return uuid.Nil, uuid.Nil, errors.New("valid actor_uuid and feed_uuid are required")
	}
	return actor, feed, nil
}

func parseFollowRequestActionIDs(actorRaw, feedRaw, targetRaw string) (actor, feed, target uuid.UUID, err error) {
	actor, feed, err = parseFollowRequestIDs(actorRaw, feedRaw)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	target, targetErr := uuid.FromString(targetRaw)
	if targetErr != nil || target == uuid.Nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, errors.New("valid target_uuid is required")
	}
	return actor, feed, target, nil
}

// RequestFollow files a pending follow request against a private feed (user
// feed or Group) on actor's own behalf. Requesting is always self-service.
func (s *ApiServer) RequestFollow(ctx context.Context, request *pb.RequestFollowRequest) (*pb.RequestFollowResponse, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, feed, err := parseFollowRequestIDs(request.ActorUuid, request.FeedUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	following, err := model.IsFollower(s.rdb, feed, actor)
	if err != nil {
		return nil, taskRPCError(err)
	}
	if following {
		return &pb.RequestFollowResponse{Requested: false}, nil
	}
	if err := s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		return model.StageRequestFollow(s.rdb, batch, feed, actor, time.Now())
	}); err != nil {
		return nil, taskRPCError(followRequestModelError(err))
	}
	return &pb.RequestFollowResponse{Requested: true}, nil
}

// CancelFollowRequest withdraws actor's own pending request. Idempotent.
func (s *ApiServer) CancelFollowRequest(ctx context.Context, request *pb.RequestFollowRequest) (*emptypb.Empty, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, feed, err := parseFollowRequestIDs(request.ActorUuid, request.FeedUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	if err := s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		return model.StageDeleteFollowRequest(s.rdb, batch, feed, actor)
	}); err != nil {
		return nil, taskRPCError(followRequestModelError(err))
	}
	return &emptypb.Empty{}, nil
}

// ApproveFollowRequest converts target's pending request into the actual
// Follow edges. actor must be the feed owner (user feed) or a Group
// admin/super. The edges and the requester's Home rebuild task commit in one
// Pebble batch, matching the JoinGroup timeline rule.
func (s *ApiServer) ApproveFollowRequest(ctx context.Context, action *pb.FollowRequestAction) (*emptypb.Empty, error) {
	if action == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, feed, target, err := parseFollowRequestActionIDs(action.ActorUuid, action.FeedUuid, action.TargetUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	spec, err := homeRebuildSpec(target)
	if err != nil {
		return nil, taskRPCError(err)
	}
	if _, err := s.tasks.EnqueueWith(ctx, []taskqueue.Spec{spec}, func(batch *pebble.Batch) error {
		if err := s.authorizeFollowRequestManage(actor, feed); err != nil {
			return err
		}
		return model.StageApproveFollowRequest(s.rdb, batch, feed, target)
	}); err != nil {
		return nil, taskRPCError(followRequestModelError(err))
	}
	return &emptypb.Empty{}, nil
}

// RejectFollowRequest deletes target's pending request. actor must be the
// feed owner (user feed) or a Group admin/super. Idempotent; the requester
// may file a new request afterwards.
func (s *ApiServer) RejectFollowRequest(ctx context.Context, action *pb.FollowRequestAction) (*emptypb.Empty, error) {
	if action == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, feed, target, err := parseFollowRequestActionIDs(action.ActorUuid, action.FeedUuid, action.TargetUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	if err := s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		if err := s.authorizeFollowRequestManage(actor, feed); err != nil {
			return err
		}
		return model.StageDeleteFollowRequest(s.rdb, batch, feed, target)
	}); err != nil {
		return nil, taskRPCError(followRequestModelError(err))
	}
	return &emptypb.Empty{}, nil
}

// ListFollowRequests pages the pending requests against one feed. actor must
// be allowed to approve them (owner / Group admin / super).
func (s *ApiServer) ListFollowRequests(ctx context.Context, request *pb.ListFollowRequestsRequest) (*pb.ListFollowRequestsResponse, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, feed, err := parseFollowRequestIDs(request.ActorUuid, request.FeedUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}
	if err := s.authorizeFollowRequestManage(actor, feed); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}

	limit := int(request.Limit)
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	var cursor uuid.UUID
	if request.Cursor != "" {
		cursor, err = uuid.FromString(request.Cursor)
		if err != nil {
			return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("invalid cursor")))
		}
	}

	entries, nextCursor, err := model.ListFollowRequests(s.rdb, feed, limit, cursor)
	if err != nil {
		return nil, taskRPCError(err)
	}
	response := &pb.ListFollowRequestsResponse{NextCursor: nextCursor}
	for _, entry := range entries {
		profile, err := model.GetProfileFromUuid(s.rdb, entry.Requester)
		if err != nil {
			continue // Skip missing/deleted requester; audit reports orphans
		}
		response.Requests = append(response.Requests, &pb.FollowRequestItem{
			Requester:   profile,
			RequestedAt: entry.RequestedAt,
		})
	}
	return response, nil
}

// followRequestModelError maps the model layer's follow-request errors onto
// the taskqueue error vocabulary taskRPCError understands.
func followRequestModelError(err error) error {
	switch {
	case errors.Is(err, model.ErrNotFound), errors.Is(err, model.ErrFollowRequestNotFound):
		return errors.Join(taskqueue.ErrNotFound, err)
	case errors.Is(err, model.ErrFollowTargetNotPrivate), errors.Is(err, model.ErrPrivateGroupUnsupported):
		return errors.Join(taskqueue.ErrFailedPrecondition, err)
	default:
		return errors.Join(taskqueue.ErrFailedPrecondition, err)
	}
}

// withPendingFollowRequest marks the feed when the viewer has a pending
// follow request against it. Best-effort: unparseable IDs and read failures
// leave the flag false.
func (s *ApiServer) withPendingFollowRequest(feed *pb.Feed, viewerRaw string) {
	if viewerRaw == "" {
		return
	}
	viewer, err := uuid.FromString(viewerRaw)
	if err != nil || viewer == uuid.Nil {
		return
	}
	target, err := uuid.FromString(feed.Uuid)
	if err != nil || target == uuid.Nil {
		return
	}
	pending, err := model.IsFollowRequestPending(s.rdb, target, viewer)
	if err == nil {
		feed.HasPendingFollowRequest = pending
	}
}
