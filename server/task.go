package server

import (
	"context"

	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Task RPCs are introduced additively. Their Queue-backed implementations are
// enabled in the server integration checkpoint; until then callers receive an
// explicit Unimplemented response rather than falling through legacy FeedJob.
func (s *ApiServer) EnqueueTask(context.Context, *pb.EnqueueTaskRequest) (*pb.EnqueueTaskResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Task queue is not enabled")
}

func (s *ApiServer) ClaimTasks(context.Context, *pb.ClaimTasksRequest) (*pb.ClaimTasksResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Task queue is not enabled")
}

func (s *ApiServer) CompleteTask(context.Context, *pb.CompleteTaskRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "Task queue is not enabled")
}

func (s *ApiServer) FailTask(context.Context, *pb.FailTaskRequest) (*pb.FailTaskResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Task queue is not enabled")
}

func (s *ApiServer) RenewTaskLease(context.Context, *pb.RenewTaskLeaseRequest) (*pb.Task, error) {
	return nil, status.Error(codes.Unimplemented, "Task queue is not enabled")
}
