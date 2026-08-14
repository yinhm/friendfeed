package server

import (
	"context"

	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *ApiServer) SubscribeService(context.Context, *pb.SubscribeServiceRequest) (*pb.Subscription, error) {
	return nil, status.Error(codes.Unimplemented, "RSS subscriptions are not enabled")
}

func (s *ApiServer) UnsubscribeService(context.Context, *pb.UnsubscribeServiceRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "RSS subscriptions are not enabled")
}

func (s *ApiServer) ListSubscriptions(context.Context, *pb.ListSubscriptionsRequest) (*pb.ListSubscriptionsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "RSS subscriptions are not enabled")
}
