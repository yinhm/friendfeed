package server

import (
	"context"
	"errors"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *ApiServer) SubscribeService(ctx context.Context, request *pb.SubscribeServiceRequest) (*pb.Subscription, error) {
	if request == nil || request.Url == "" {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	userID, err := uuid.FromString(request.UserUuid)
	if err != nil || userID == uuid.Nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("valid user_uuid is required")))
	}
	if _, err := model.NormalizeSubscriptionURL(request.Url); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	subscription, err := model.SubscribeRSS(s.rdb, userID, request.Url, s.rssNow())
	if err != nil {
		return nil, taskRPCError(err)
	}
	payload, err := proto.Marshal(&pb.RSSFetchPayload{FeedUuid: subscription.FeedUuid})
	if err != nil {
		return nil, taskRPCError(err)
	}
	if _, err := s.tasks.Enqueue(ctx, taskqueue.Spec{
		Type: rssFetchTaskType, Payload: payload, PayloadVersion: 1,
		IdempotencyKey: subscription.FeedUuid,
	}); err != nil && !errors.Is(err, taskqueue.ErrClosed) {
		return nil, taskRPCError(err)
	}
	return subscription, nil
}

func (s *ApiServer) UnsubscribeService(ctx context.Context, request *pb.UnsubscribeServiceRequest) (*emptypb.Empty, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	userID, userErr := uuid.FromString(request.UserUuid)
	feedID, feedErr := uuid.FromString(request.FeedUuid)
	if userErr != nil || feedErr != nil || userID == uuid.Nil || feedID == uuid.Nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("valid user_uuid and feed_uuid are required")))
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}
	if err := model.UnsubscribeRSS(s.rdb, userID, feedID); err != nil {
		return nil, taskRPCError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ApiServer) ListSubscriptions(ctx context.Context, request *pb.ListSubscriptionsRequest) (*pb.ListSubscriptionsResponse, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	userID, err := uuid.FromString(request.UserUuid)
	if err != nil || userID == uuid.Nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("valid user_uuid is required")))
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}
	subscriptions, err := model.ListRSSSubscriptions(s.rdb, userID)
	if err != nil {
		return nil, taskRPCError(err)
	}
	return &pb.ListSubscriptionsResponse{Subscriptions: subscriptions}, nil
}
