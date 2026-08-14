package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const taskReapInterval = time.Minute
const taskWorkerConcurrency = 4

func (s *ApiServer) EnqueueTask(ctx context.Context, request *pb.EnqueueTaskRequest) (*pb.EnqueueTaskResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	result, err := s.tasks.Enqueue(ctx, taskqueue.Spec{
		Type: request.Type, Payload: request.Payload, PayloadVersion: request.PayloadVersion,
		IdempotencyKey: request.IdempotencyKey, RunAtMS: request.RunAtMs,
	})
	if err != nil {
		return nil, taskRPCError(err)
	}
	return &pb.EnqueueTaskResponse{Task: result.Task, AlreadyExists: result.AlreadyExists}, nil
}

func (s *ApiServer) ClaimTasks(ctx context.Context, request *pb.ClaimTasksRequest) (*pb.ClaimTasksResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	tasks, err := s.tasks.Claim(ctx, request.WorkerId, request.Types, int(request.MaxTasks))
	if err != nil {
		return nil, taskRPCError(err)
	}
	return &pb.ClaimTasksResponse{Tasks: tasks}, nil
}

func (s *ApiServer) CompleteTask(ctx context.Context, request *pb.CompleteTaskRequest) (*emptypb.Empty, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.tasks.Complete(ctx, request.WorkerId, request.TaskId, request.LeaseEpoch); err != nil {
		return nil, taskRPCError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ApiServer) FailTask(ctx context.Context, request *pb.FailTaskRequest) (*pb.FailTaskResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	result, err := s.tasks.Fail(ctx, request.WorkerId, request.TaskId, request.LeaseEpoch, request.Error)
	if err != nil {
		return nil, taskRPCError(err)
	}
	return &pb.FailTaskResponse{Outcome: result.Outcome, NextRunAtMs: result.NextRunAt}, nil
}

func (s *ApiServer) RenewTaskLease(ctx context.Context, request *pb.RenewTaskLeaseRequest) (*pb.Task, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	task, err := s.tasks.Renew(ctx, request.WorkerId, request.TaskId, request.LeaseEpoch)
	if err != nil {
		return nil, taskRPCError(err)
	}
	return task, nil
}

func taskRPCError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	case errors.Is(err, taskqueue.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, taskqueue.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, taskqueue.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, taskqueue.ErrClosed):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

func (s *ApiServer) StopTaskClaims() {
	if s.tasks != nil {
		s.tasks.StopAccepting()
	}
}

func (s *ApiServer) TaskReapLoop() {
	if !s.beginBackgroundJob() {
		return
	}
	defer s.wg.Done()
	if err := s.tasks.ReapLoop(s.taskCtx, taskReapInterval); err != nil {
		slog.Error("task reaper stopped", "error", err)
	}
}

// StartTaskWorkers starts the bounded in-process consumer pool for registered
// task types that provide handlers. It is idempotent.
func (s *ApiServer) StartTaskWorkers() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.startTaskWorkersLocked()
}

func (s *ApiServer) startTaskWorkersLocked() {
	if s.taskWorkersStarted || s.shuttingDown {
		return
	}
	types := s.tasks.TypesWithHandlers()
	if len(types) == 0 {
		return
	}
	s.taskWorkersStarted = true
	for i := 0; i < taskWorkerConcurrency; i++ {
		go s.taskWorkerLoop(fmt.Sprintf("ffdb-%d", i+1), types)
	}
}

func (s *ApiServer) taskWorkerLoop(workerID string, types []string) {
	if !s.beginBackgroundJob() {
		return
	}
	defer s.wg.Done()
	delay := s.taskWorkerPollMin
	for {
		select {
		case <-s.taskCtx.Done():
			return
		default:
		}
		claimed, err := s.tasks.Claim(s.taskCtx, workerID, types, 1)
		if errors.Is(err, taskqueue.ErrClosed) || errors.Is(err, context.Canceled) {
			return
		}
		if err != nil {
			slog.Error("task claim failed", "worker", workerID, "error", err)
			if !waitTaskWorker(s.taskCtx, delay) {
				return
			}
			delay = min(delay*2, s.taskWorkerPollMax)
			continue
		}
		if len(claimed) == 0 {
			if !waitTaskWorker(s.taskCtx, delay) {
				return
			}
			delay = min(delay*2, s.taskWorkerPollMax)
			continue
		}
		delay = s.taskWorkerPollMin
		s.executeClaimedTask(workerID, claimed[0])
	}
}

func (s *ApiServer) executeClaimedTask(workerID string, claimed *pb.Task) {
	definition, ok := s.tasks.Definition(claimed.Type)
	if !ok || definition.Handler == nil {
		_, _ = s.tasks.Fail(s.taskCtx, workerID, claimed.Id, claimed.LeaseEpoch, "no handler registered")
		return
	}
	handlerCtx, cancelHandler := context.WithCancel(s.taskCtx)
	renewDone := make(chan error, 1)
	go func() {
		renewDone <- s.renewClaimedTask(handlerCtx, cancelHandler, workerID, claimed.Id, claimed.LeaseEpoch, claimed.LeaseUntilMs)
	}()
	handlerErr := callTaskHandler(handlerCtx, definition.Handler, claimed)
	cancelHandler()
	renewErr := <-renewDone
	if s.taskCtx.Err() != nil {
		return
	}
	if renewErr != nil {
		slog.Warn("task lease renewal failed", "task_id", claimed.Id, "type", claimed.Type, "error", renewErr)
		return
	}
	if handlerErr != nil {
		if _, err := s.tasks.Fail(s.taskCtx, workerID, claimed.Id, claimed.LeaseEpoch, handlerErr.Error()); err != nil {
			slog.Warn("task failure could not be recorded", "task_id", claimed.Id, "type", claimed.Type, "error", err)
		}
		return
	}
	if err := s.tasks.Complete(s.taskCtx, workerID, claimed.Id, claimed.LeaseEpoch); err != nil {
		slog.Warn("task completion could not be recorded", "task_id", claimed.Id, "type", claimed.Type, "error", err)
	}
}

func (s *ApiServer) renewClaimedTask(ctx context.Context, cancelHandler context.CancelFunc, workerID, taskID string, epoch uint64, leaseUntilMS int64) error {
	for {
		remaining := time.Until(time.UnixMilli(leaseUntilMS))
		if remaining <= 0 {
			cancelHandler()
			return taskqueue.ErrFailedPrecondition
		}
		timer := time.NewTimer(max(remaining/2, 10*time.Millisecond))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
			renewed, err := s.tasks.Renew(ctx, workerID, taskID, epoch)
			if err != nil {
				cancelHandler()
				return err
			}
			leaseUntilMS = renewed.LeaseUntilMs
		}
	}
}

func callTaskHandler(ctx context.Context, handler taskqueue.Handler, claimed *pb.Task) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("handler panic: %v\n%s", recovered, debug.Stack())
		}
	}()
	return handler(ctx, claimed)
}

func waitTaskWorker(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
