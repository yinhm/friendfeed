package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *ApiServer) authorizeFeedServiceAdmin(actorID, targetID uuid.UUID) error {
	actor, err := model.GetProfileFromUuid(s.rdb, actorID)
	if err != nil {
		return err
	}
	target, err := model.GetProfileFromUuid(s.rdb, targetID)
	if err != nil {
		return err
	}
	if actor.IsSuper || (target.Type == "user" && actorID == targetID) {
		return nil
	}
	if target.Type != "group" {
		return errors.New("FeedService target is not manageable by actor")
	}
	isAdmin, err := model.IsGroupAdmin(s.rdb, targetID, actorID)
	if err != nil {
		return err
	}
	if !isAdmin {
		return errors.New("actor is not a group administrator")
	}
	return nil
}

func parseFeedServiceRequestIDs(actorRaw, targetRaw string) (uuid.UUID, uuid.UUID, error) {
	actor, actorErr := uuid.FromString(actorRaw)
	target, targetErr := uuid.FromString(targetRaw)
	if actorErr != nil || targetErr != nil || actor == uuid.Nil || target == uuid.Nil {
		return uuid.Nil, uuid.Nil, errors.New("valid actor_uuid and target_feed_uuid are required")
	}
	return actor, target, nil
}

func (s *ApiServer) AddFeedService(ctx context.Context, request *pb.AddFeedServiceRequest) (*pb.FeedService, error) {
	if request == nil || request.Url == "" || request.Kind != model.WebFeedServiceKind {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, target, err := parseFeedServiceRequestIDs(request.ActorUuid, request.TargetFeedUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	now := s.rssNow().UTC()
	_, serviceUUID, err := model.ServiceIdentity(request.Kind, request.Url)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	payload, err := proto.Marshal(&pb.FeedServiceSeedPayload{
		ServiceUuid: serviceUUID.String(), TargetFeedUuid: target.String(), ServiceId: serviceUUID.String(),
	})
	if err != nil {
		return nil, taskRPCError(err)
	}
	var binding *pb.FeedService
	_, err = s.tasks.EnqueueWith(ctx, []taskqueue.Spec{{
		Type: feedServiceSeedTaskType, Payload: payload, PayloadVersion: 1,
		IdempotencyKey: target.String() + ":" + serviceUUID.String(),
	}}, func(batch *pebble.Batch) error {
		if err := s.authorizeFeedServiceAdmin(actor, target); err != nil {
			return errors.Join(taskqueue.ErrFailedPrecondition, err)
		}
		created, _, err := model.StageAddWebFeedService(s.rdb, batch, target, actor, request.Url, now)
		binding = created
		return err
	})
	if err != nil {
		return nil, taskRPCError(err)
	}
	if binding == nil {
		return nil, taskRPCError(fmt.Errorf("FeedService was not created"))
	}
	return binding, nil
}

func (s *ApiServer) RemoveFeedService(ctx context.Context, request *pb.RemoveFeedServiceRequest) (*emptypb.Empty, error) {
	if request == nil || request.ServiceId == "" {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, target, err := parseFeedServiceRequestIDs(request.ActorUuid, request.TargetFeedUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}
	if err := s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		if err := s.authorizeFeedServiceAdmin(actor, target); err != nil {
			return errors.Join(taskqueue.ErrFailedPrecondition, err)
		}
		return model.StageRemoveFeedService(s.rdb, batch, target, request.ServiceId)
	}); err != nil {
		return nil, taskRPCError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ApiServer) ListFeedServices(ctx context.Context, request *pb.ListFeedServicesRequest) (*pb.ListFeedServicesResponse, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, target, err := parseFeedServiceRequestIDs(request.ActorUuid, request.TargetFeedUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	if err := s.authorizeFeedServiceAdmin(actor, target); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}
	services, err := model.ListFeedServices(s.rdb, target)
	if err != nil {
		return nil, taskRPCError(err)
	}
	states := make(map[string]*pb.ServiceState)
	for _, binding := range services {
		serviceID, err := uuid.FromString(binding.ServiceUuid)
		if err != nil {
			continue
		}
		state, err := model.GetServiceState(s.rdb, serviceID)
		if err == nil {
			states[binding.ServiceUuid] = state
		}
	}
	return &pb.ListFeedServicesResponse{Services: services, States: states}, nil
}

func (s *ApiServer) SetFeedServiceEnabled(ctx context.Context, request *pb.SetFeedServiceEnabledRequest) (*pb.FeedService, error) {
	if request == nil || request.ServiceId == "" {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, target, err := parseFeedServiceRequestIDs(request.ActorUuid, request.TargetFeedUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	var binding *pb.FeedService
	err = s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		if err := s.authorizeFeedServiceAdmin(actor, target); err != nil {
			return errors.Join(taskqueue.ErrFailedPrecondition, err)
		}
		var stageErr error
		binding, stageErr = model.StageSetFeedServiceEnabled(s.rdb, batch, target, request.ServiceId, request.Enabled)
		return stageErr
	})
	if err != nil {
		return nil, taskRPCError(err)
	}
	return binding, nil
}

func (s *ApiServer) RefreshFeedService(ctx context.Context, request *pb.RefreshFeedServiceRequest) (*emptypb.Empty, error) {
	if request == nil || request.ServiceId == "" {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, target, err := parseFeedServiceRequestIDs(request.ActorUuid, request.TargetFeedUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	// Protect the binding lookup from unauthorized callers; EnqueueWith
	// repeats this check inside its atomic mutation boundary.
	if err := s.authorizeFeedServiceAdmin(actor, target); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	binding, err := model.GetFeedService(s.rdb, target, request.ServiceId)
	if err != nil {
		return nil, taskRPCError(err)
	}
	if !binding.Enabled {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, errors.New("FeedService is disabled")))
	}
	payload, err := proto.Marshal(&pb.FeedServiceSeedPayload{
		ServiceUuid: binding.ServiceUuid, TargetFeedUuid: target.String(), ServiceId: binding.Id,
	})
	if err != nil {
		return nil, taskRPCError(err)
	}
	_, err = s.tasks.EnqueueWith(ctx, []taskqueue.Spec{{
		Type: feedServiceSeedTaskType, Payload: payload, PayloadVersion: 1,
		IdempotencyKey: target.String() + ":" + binding.Id,
	}}, func(_ *pebble.Batch) error {
		if err := s.authorizeFeedServiceAdmin(actor, target); err != nil {
			return errors.Join(taskqueue.ErrFailedPrecondition, err)
		}
		current, err := model.GetFeedService(s.rdb, target, request.ServiceId)
		if err != nil {
			return err
		}
		if !current.Enabled || current.ServiceUuid != binding.ServiceUuid {
			return errors.New("FeedService changed before refresh could be queued")
		}
		return nil
	})
	if err != nil {
		return nil, taskRPCError(err)
	}
	return &emptypb.Empty{}, nil
}
