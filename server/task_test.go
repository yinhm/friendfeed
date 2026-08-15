package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	taskqueue "github.com/yinhm/friendfeed/task"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (s *RpcTestSuite) taskClient() (pb.ApiClient, func()) {
	conn, err := grpc.Dial(s.rpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)
	return pb.NewApiClient(conn), func() { s.Require().NoError(conn.Close()) }
}

func (s *RpcTestSuite) enableTestTaskQueue() {
	registry, err := taskqueue.NewRegistry(map[string]taskqueue.Definition{
		"test.run": {
			MaxAttempts: 2, LeaseDuration: time.Minute, MaxLease: 5 * time.Minute,
			BackoffBase: time.Second, BackoffCap: time.Minute,
		},
	})
	s.Require().NoError(err)
	queue, err := taskqueue.NewQueue(s.srv.rdb, registry, taskqueue.Options{
		Jitter: func(delay time.Duration) time.Duration { return delay },
	})
	s.Require().NoError(err)
	s.srv.tasks = queue
}

func (s *RpcTestSuite) TestTaskRPCQueueLifecycle() {
	s.enableTestTaskQueue()
	client, closeClient := s.taskClient()
	defer closeClient()
	ctx := context.Background()

	enqueued, err := client.EnqueueTask(ctx, &pb.EnqueueTaskRequest{
		Type: "test.run", Payload: []byte("payload"), IdempotencyKey: "same",
	})
	s.Require().NoError(err)
	s.False(enqueued.AlreadyExists)
	duplicate, err := client.EnqueueTask(ctx, &pb.EnqueueTaskRequest{
		Type: "test.run", Payload: []byte("ignored"), IdempotencyKey: "same",
	})
	s.Require().NoError(err)
	s.True(duplicate.AlreadyExists)
	s.Equal(enqueued.Task.Id, duplicate.Task.Id)

	claimed, err := client.ClaimTasks(ctx, &pb.ClaimTasksRequest{
		WorkerId: "rpc-worker", Types: []string{"test.run"}, MaxTasks: 1,
	})
	s.Require().NoError(err)
	s.Require().Len(claimed.Tasks, 1)
	task := claimed.Tasks[0]

	_, err = client.CompleteTask(ctx, &pb.CompleteTaskRequest{
		WorkerId: "rpc-worker", TaskId: task.Id, LeaseEpoch: task.LeaseEpoch + 1,
	})
	s.Equal(codes.FailedPrecondition, status.Code(err))
	_, err = client.CompleteTask(ctx, &pb.CompleteTaskRequest{
		WorkerId: "rpc-worker", TaskId: task.Id, LeaseEpoch: task.LeaseEpoch,
	})
	s.Require().NoError(err)
}

func (s *RpcTestSuite) TestTaskRPCValidationAndErrorMapping() {
	s.enableTestTaskQueue()
	client, closeClient := s.taskClient()
	defer closeClient()
	ctx := context.Background()

	_, err := client.EnqueueTask(ctx, &pb.EnqueueTaskRequest{Type: "unknown"})
	s.Equal(codes.InvalidArgument, status.Code(err))
	_, err = client.ClaimTasks(ctx, &pb.ClaimTasksRequest{})
	s.Equal(codes.InvalidArgument, status.Code(err))
	_, err = client.CompleteTask(ctx, &pb.CompleteTaskRequest{
		WorkerId: "worker", TaskId: "000102030405060708090a0b0c0d0e0f", LeaseEpoch: 1,
	})
	s.Equal(codes.NotFound, status.Code(err))

	s.srv.StopTaskClaims()
	_, err = client.EnqueueTask(ctx, &pb.EnqueueTaskRequest{Type: "test.run"})
	s.Equal(codes.Unavailable, status.Code(err))
}

func TestTaskRPCErrorMapsContext(t *testing.T) {
	require.Equal(t, codes.Canceled, status.Code(taskRPCError(context.Canceled)))
	require.Equal(t, codes.DeadlineExceeded, status.Code(taskRPCError(context.DeadlineExceeded)))
	require.Equal(t, codes.Internal, status.Code(taskRPCError(taskqueue.ErrCorrupt)))
}

func (s *RpcTestSuite) installTaskHandler(definition taskqueue.Definition) {
	registry, err := taskqueue.NewRegistry(map[string]taskqueue.Definition{"test.run": definition})
	s.Require().NoError(err)
	queue, err := taskqueue.NewQueue(s.srv.rdb, registry, taskqueue.Options{
		Jitter: func(delay time.Duration) time.Duration { return delay },
	})
	s.Require().NoError(err)
	s.srv.tasks = queue
	s.srv.taskWorkerPollMin = time.Millisecond
	s.srv.taskWorkerPollMax = 5 * time.Millisecond
}

func (s *RpcTestSuite) TestTaskWorkerRenewsAndCompletes() {
	var calls atomic.Int32
	definition := taskqueue.Definition{
		MaxAttempts: 2, LeaseDuration: 250 * time.Millisecond, MaxLease: 2 * time.Second,
		BackoffBase: time.Millisecond, BackoffCap: time.Second,
		Handler: func(ctx context.Context, task *pb.Task) error {
			calls.Add(1)
			select {
			case <-time.After(600 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	s.installTaskHandler(definition)
	s.srv.StartTaskWorkers()
	_, err := s.srv.tasks.Enqueue(context.Background(), taskqueue.Spec{Type: "test.run"})
	s.Require().NoError(err)
	s.Require().Eventually(func() bool {
		return taskCompletionCount(s.srv.rdb, pb.TaskCompletionStatus_TASK_COMPLETION_STATUS_OK) == 1
	}, 3*time.Second, 10*time.Millisecond)
	s.EqualValues(1, calls.Load())
}

func (s *RpcTestSuite) TestTaskWorkerPanicMovesTaskToDead() {
	definition := taskqueue.Definition{
		MaxAttempts: 1, LeaseDuration: time.Second, MaxLease: time.Minute,
		BackoffBase: time.Millisecond, BackoffCap: time.Second,
		Handler: func(context.Context, *pb.Task) error { panic("boom") },
	}
	s.installTaskHandler(definition)
	s.srv.StartTaskWorkers()
	_, err := s.srv.tasks.Enqueue(context.Background(), taskqueue.Spec{Type: "test.run"})
	s.Require().NoError(err)
	s.Require().Eventually(func() bool {
		return taskCompletionCount(s.srv.rdb, pb.TaskCompletionStatus_TASK_COMPLETION_STATUS_DEAD) == 1
	}, 3*time.Second, 10*time.Millisecond)
}

func (s *RpcTestSuite) TestTaskWorkerShutdownCancelsHandler() {
	started := make(chan struct{})
	definition := taskqueue.Definition{
		MaxAttempts: 2, LeaseDuration: time.Second, MaxLease: time.Minute,
		BackoffBase: time.Millisecond, BackoffCap: time.Second,
		Handler: func(ctx context.Context, _ *pb.Task) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	}
	s.installTaskHandler(definition)
	s.srv.StartTaskWorkers()
	_, err := s.srv.tasks.Enqueue(context.Background(), taskqueue.Spec{Type: "test.run"})
	s.Require().NoError(err)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		s.T().Fatal("task handler did not start")
	}
	done := make(chan struct{})
	go func() {
		s.srv.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		s.T().Fatal("Shutdown did not cancel task handler")
	}
}

func taskCompletionCount(db *store.Store, status pb.TaskCompletionStatus) int {
	count := 0
	_, err := db.ForwardScan(model.TaskDone.Prefix, func(_ int, _, value []byte) error {
		completion := new(pb.TaskCompletion)
		if err := proto.Unmarshal(value, completion); err != nil {
			return err
		}
		if completion.Status == status {
			count++
		}
		return nil
	})
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return -1
	}
	return count
}
