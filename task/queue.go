package task

import (
	"bytes"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/pebble/v2"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/store/flake"
	"google.golang.org/protobuf/proto"
)

var (
	ErrInvalidArgument    = errors.New("task: invalid argument")
	ErrNotFound           = errors.New("task: not found")
	ErrFailedPrecondition = errors.New("task: failed precondition")
	ErrCorrupt            = errors.New("task: corrupt queue")
)

const (
	DefaultMaxClaimTasks = 32
	DefaultReapLimit     = 100
	MaxWorkerIDBytes     = 128
	MaxTaskErrorBytes    = 1024
)

type Spec struct {
	Type           string
	Payload        []byte
	PayloadVersion uint32
	IdempotencyKey string
	RunAtMS        int64
}

type EnqueueResult struct {
	Task          *pb.Task
	AlreadyExists bool
}

type FailResult struct {
	Outcome   pb.FailTaskOutcome
	NextRunAt int64
}

type Queue struct {
	db        *store.Store
	registry  *Registry
	mu        sync.Mutex
	now       func() time.Time
	jitter    func(time.Duration) time.Duration
	maxClaim  int
	reapLimit int
}

type Options struct {
	Now       func() time.Time
	Jitter    func(time.Duration) time.Duration
	MaxClaim  int
	ReapLimit int
}

func NewQueue(db *store.Store, registry *Registry, options Options) (*Queue, error) {
	if db == nil || registry == nil {
		return nil, fmt.Errorf("%w: database and registry are required", ErrInvalidArgument)
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Jitter == nil {
		options.Jitter = func(delay time.Duration) time.Duration {
			percent := rand.Int64N(41) - 20
			return delay + (delay/100)*time.Duration(percent)
		}
	}
	if options.MaxClaim <= 0 {
		options.MaxClaim = DefaultMaxClaimTasks
	}
	if options.ReapLimit <= 0 {
		options.ReapLimit = DefaultReapLimit
	}
	return &Queue{
		db: db, registry: registry, now: options.Now, jitter: options.Jitter,
		maxClaim: options.MaxClaim, reapLimit: options.ReapLimit,
	}, nil
}

func (q *Queue) Enqueue(ctx context.Context, spec Spec) (EnqueueResult, error) {
	results, err := q.EnqueueWith(ctx, []Spec{spec}, nil)
	if err != nil {
		return EnqueueResult{}, err
	}
	return results[0], nil
}

func (q *Queue) EnqueueWith(ctx context.Context, specs []Spec, business func(*pebble.Batch) error) ([]EnqueueResult, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: at least one task spec is required", ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	nowMS := q.now().UTC().UnixMilli()
	if nowMS < 0 {
		return nil, fmt.Errorf("%w: current time is before Unix epoch", ErrInvalidArgument)
	}

	results := make([]EnqueueResult, len(specs))
	newTasks := make([]*pb.Task, 0, len(specs))
	localIdem := make(map[string]*pb.Task)
	for i, spec := range specs {
		definition, err := q.registry.Validate(spec.Type, spec.Payload, spec.PayloadVersion)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
		}
		runAt := spec.RunAtMS
		if runAt == 0 {
			runAt = nowMS
		}
		if runAt < 0 {
			return nil, fmt.Errorf("%w: run_at is before Unix epoch", ErrInvalidArgument)
		}

		if spec.IdempotencyKey != "" {
			idemKey, err := IdemKey(spec.Type, spec.IdempotencyKey)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
			}
			localKey := string(idemKey)
			if existing := localIdem[localKey]; existing != nil {
				results[i] = EnqueueResult{Task: cloneTask(existing), AlreadyExists: true}
				continue
			}
			existing, err := q.taskByIdem(idemKey, spec.Type, spec.IdempotencyKey)
			if err == nil {
				localIdem[localKey] = existing
				results[i] = EnqueueResult{Task: cloneTask(existing), AlreadyExists: true}
				continue
			}
			if !errors.Is(err, ErrNotFound) {
				return nil, err
			}
		}

		id := q.db.NextId()
		if err := validateTaskID(id); err != nil {
			return nil, fmt.Errorf("generate task id: %w", err)
		}
		task := &pb.Task{
			Id: EncodeTaskID(id), Type: spec.Type, Payload: append([]byte(nil), spec.Payload...),
			PayloadVersion: spec.PayloadVersion, IdempotencyKey: spec.IdempotencyKey,
			State: pb.TaskState_TASK_STATE_READY, RunAtMs: runAt,
			MaxAttempts: definition.MaxAttempts, CreatedAtMs: nowMS, UpdatedAtMs: nowMS,
		}
		newTasks = append(newTasks, task)
		results[i] = EnqueueResult{Task: cloneTask(task)}
		if spec.IdempotencyKey != "" {
			idemKey, _ := IdemKey(spec.Type, spec.IdempotencyKey)
			localIdem[string(idemKey)] = task
		}
	}

	err := q.db.ApplyBatch(func(batch *pebble.Batch) error {
		if business != nil {
			if err := business(batch); err != nil {
				return err
			}
		}
		for _, task := range newTasks {
			if err := q.stageNewTask(batch, task); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (q *Queue) Claim(ctx context.Context, workerID string, taskTypes []string, maxTasks int) ([]*pb.Task, error) {
	if err := validateWorker(workerID); err != nil {
		return nil, err
	}
	if len(taskTypes) == 0 || maxTasks <= 0 || maxTasks > q.maxClaim {
		return nil, fmt.Errorf("%w: types and max_tasks within 1..%d are required", ErrInvalidArgument, q.maxClaim)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	nowMS := q.now().UTC().UnixMilli()
	candidates, err := q.readyCandidates(taskTypes, nowMS, maxTasks)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return []*pb.Task{}, nil
	}
	claimed := make([]*pb.Task, 0, len(candidates))
	seen := make(map[flake.Id]struct{}, len(candidates))
	err = q.db.ApplyBatch(func(batch *pebble.Batch) error {
		for _, candidate := range candidates {
			if _, duplicate := seen[candidate.id]; duplicate {
				return fmt.Errorf("%w: duplicate Ready rows for task %s", ErrCorrupt, EncodeTaskID(candidate.id))
			}
			seen[candidate.id] = struct{}{}
			task, err := q.loadTask(candidate.id)
			if err != nil {
				return err
			}
			if task.State != pb.TaskState_TASK_STATE_READY || task.Type != candidate.taskType || task.RunAtMs != candidate.runAtMS {
				return fmt.Errorf("%w: Ready row does not match Task %s", ErrCorrupt, task.Id)
			}
			definition, ok := q.registry.Definition(task.Type)
			if !ok {
				return fmt.Errorf("%w: Task %s has unregistered type %q", ErrCorrupt, task.Id, task.Type)
			}
			if task.Attempts == math.MaxUint32 || task.LeaseEpoch == math.MaxUint64 {
				return fmt.Errorf("%w: Task %s counters overflow", ErrCorrupt, task.Id)
			}
			task.State = pb.TaskState_TASK_STATE_INFLIGHT
			task.Attempts++
			task.LeaseEpoch++
			task.ClaimedAtMs = nowMS
			task.LeaseUntilMs = nowMS + definition.LeaseDuration.Milliseconds()
			task.LeasedBy = workerID
			task.UpdatedAtMs = nowMS
			if err := batch.Delete(candidate.key, nil); err != nil {
				return err
			}
			leaseKey, err := LeaseKey(task.LeaseUntilMs, candidate.id)
			if err != nil {
				return err
			}
			if err := batch.Set(leaseKey, nil, nil); err != nil {
				return err
			}
			if err := q.stageTask(batch, candidate.id, task); err != nil {
				return err
			}
			claimed = append(claimed, cloneTask(task))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (q *Queue) Complete(ctx context.Context, workerID, taskID string, epoch uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	id, task, nowMS, err := q.leasedTask(workerID, taskID, epoch, true)
	if err != nil {
		return err
	}
	completion := &pb.TaskCompletion{
		Task: cloneTask(task), Status: pb.TaskCompletionStatus_TASK_COMPLETION_STATUS_OK,
		FinishedAtMs: nowMS,
	}
	return q.finishTask(id, task, completion)
}

func (q *Queue) Fail(ctx context.Context, workerID, taskID string, epoch uint64, failure string) (FailResult, error) {
	if err := ctx.Err(); err != nil {
		return FailResult{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	id, task, nowMS, err := q.leasedTask(workerID, taskID, epoch, true)
	if err != nil {
		return FailResult{}, err
	}
	return q.failLocked(id, task, nowMS, truncateError(failure))
}

func (q *Queue) Renew(ctx context.Context, workerID, taskID string, epoch uint64) (*pb.Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	id, task, nowMS, err := q.leasedTask(workerID, taskID, epoch, true)
	if err != nil {
		return nil, err
	}
	definition, ok := q.registry.Definition(task.Type)
	if !ok {
		return nil, fmt.Errorf("%w: Task %s has unregistered type", ErrCorrupt, task.Id)
	}
	hardLimit := task.ClaimedAtMs + definition.MaxLease.Milliseconds()
	if task.LeaseUntilMs >= hardLimit || nowMS >= hardLimit {
		return nil, fmt.Errorf("%w: Task %s reached maximum lease", ErrFailedPrecondition, task.Id)
	}
	newUntil := nowMS + definition.LeaseDuration.Milliseconds()
	if newUntil > hardLimit {
		newUntil = hardLimit
	}
	if newUntil <= task.LeaseUntilMs {
		return cloneTask(task), nil
	}
	oldLease, _ := LeaseKey(task.LeaseUntilMs, id)
	newLease, _ := LeaseKey(newUntil, id)
	task.LeaseUntilMs = newUntil
	task.UpdatedAtMs = nowMS
	err = q.db.ApplyBatch(func(batch *pebble.Batch) error {
		if err := batch.Delete(oldLease, nil); err != nil {
			return err
		}
		if err := batch.Set(newLease, nil, nil); err != nil {
			return err
		}
		return q.stageTask(batch, id, task)
	})
	if err != nil {
		return nil, err
	}
	return cloneTask(task), nil
}

func (q *Queue) ReapOnce(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	nowMS := q.now().UTC().UnixMilli()

	type expiredLease struct {
		key   store.Key
		id    flake.Id
		until int64
	}
	expired := make([]expiredLease, 0, q.reapLimit)
	iter, err := q.db.NewIterator(model.TaskLease.Prefix)
	if err != nil {
		return 0, err
	}
	for iter.First(); iter.Valid() && len(expired) < q.reapLimit; iter.Next() {
		until, id, parseErr := ParseLeaseKey(iter.UnsafeKey())
		if parseErr != nil {
			_ = iter.Close()
			return 0, fmt.Errorf("%w: %v", ErrCorrupt, parseErr)
		}
		if until > nowMS {
			break
		}
		expired = append(expired, expiredLease{key: iter.Key(), id: id, until: until})
	}
	iterErr := iter.Error()
	closeErr := iter.Close()
	if err := errors.Join(iterErr, closeErr); err != nil {
		return 0, err
	}
	if len(expired) == 0 {
		return 0, nil
	}

	reaped := 0
	err = q.db.ApplyBatch(func(batch *pebble.Batch) error {
		for _, lease := range expired {
			task, err := q.loadTask(lease.id)
			if err != nil {
				return err
			}
			if task.State != pb.TaskState_TASK_STATE_INFLIGHT || task.LeaseUntilMs != lease.until {
				return fmt.Errorf("%w: Lease row does not match Task %s", ErrCorrupt, task.Id)
			}
			if err := q.stageFailedTask(batch, lease.id, task, nowMS, "lease_lost"); err != nil {
				return err
			}
			reaped++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return reaped, nil
}

func (q *Queue) stageNewTask(batch *pebble.Batch, task *pb.Task) error {
	id, err := DecodeTaskID(task.Id)
	if err != nil {
		return err
	}
	readyKey, err := ReadyKey(task.Type, task.RunAtMs, id)
	if err != nil {
		return err
	}
	if err := q.stageTask(batch, id, task); err != nil {
		return err
	}
	if err := batch.Set(readyKey, nil, nil); err != nil {
		return err
	}
	if task.IdempotencyKey != "" {
		idemKey, err := IdemKey(task.Type, task.IdempotencyKey)
		if err != nil {
			return err
		}
		if err := batch.Set(idemKey, id[:], nil); err != nil {
			return err
		}
	}
	return nil
}

func (q *Queue) stageTask(batch *pebble.Batch, id flake.Id, task *pb.Task) error {
	key, err := TaskKey(id)
	if err != nil {
		return err
	}
	raw, err := proto.Marshal(task)
	if err != nil {
		return err
	}
	return batch.Set(key, raw, nil)
}

func (q *Queue) loadTask(id flake.Id) (*pb.Task, error) {
	key, err := TaskKey(id)
	if err != nil {
		return nil, err
	}
	raw, err := q.db.Get(key)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: Task %s", ErrNotFound, EncodeTaskID(id))
	}
	if err != nil {
		return nil, err
	}
	task := new(pb.Task)
	if err := proto.Unmarshal(raw, task); err != nil {
		return nil, fmt.Errorf("%w: decode Task %s: %v", ErrCorrupt, EncodeTaskID(id), err)
	}
	if task.Id != EncodeTaskID(id) {
		return nil, fmt.Errorf("%w: Task key/id mismatch", ErrCorrupt)
	}
	return task, nil
}

func (q *Queue) taskByIdem(idemKey store.Key, taskType, idempotencyKey string) (*pb.Task, error) {
	raw, err := q.db.Get(idemKey)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(raw) != TaskIDSize {
		return nil, fmt.Errorf("%w: Idem value length %d", ErrCorrupt, len(raw))
	}
	var id flake.Id
	copy(id[:], raw)
	task, err := q.loadTask(id)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("%w: Idem points to missing Task", ErrCorrupt)
	}
	if err != nil {
		return nil, err
	}
	if task.Type != taskType || task.IdempotencyKey != idempotencyKey ||
		(task.State != pb.TaskState_TASK_STATE_READY && task.State != pb.TaskState_TASK_STATE_INFLIGHT) {
		return nil, fmt.Errorf("%w: Idem does not match active Task %s", ErrCorrupt, task.Id)
	}
	return task, nil
}

func (q *Queue) leasedTask(workerID, taskID string, epoch uint64, requireCurrent bool) (flake.Id, *pb.Task, int64, error) {
	if err := validateWorker(workerID); err != nil {
		return flake.Id{}, nil, 0, err
	}
	id, err := DecodeTaskID(taskID)
	if err != nil {
		return flake.Id{}, nil, 0, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	task, err := q.loadTask(id)
	if err != nil {
		return flake.Id{}, nil, 0, err
	}
	if task.State != pb.TaskState_TASK_STATE_INFLIGHT || task.LeasedBy != workerID || task.LeaseEpoch != epoch {
		return flake.Id{}, nil, 0, fmt.Errorf("%w: Task %s lease owner or epoch does not match", ErrFailedPrecondition, taskID)
	}
	nowMS := q.now().UTC().UnixMilli()
	if requireCurrent && nowMS >= task.LeaseUntilMs {
		return flake.Id{}, nil, 0, fmt.Errorf("%w: Task %s lease expired", ErrFailedPrecondition, taskID)
	}
	return id, task, nowMS, nil
}

func (q *Queue) finishTask(id flake.Id, task *pb.Task, completion *pb.TaskCompletion) error {
	taskKey, _ := TaskKey(id)
	leaseKey, _ := LeaseKey(task.LeaseUntilMs, id)
	doneKey, err := DoneKey(completion.FinishedAtMs, id)
	if err != nil {
		return err
	}
	raw, err := proto.Marshal(completion)
	if err != nil {
		return err
	}
	return q.db.ApplyBatch(func(batch *pebble.Batch) error {
		if err := batch.Delete(taskKey, nil); err != nil {
			return err
		}
		if err := batch.Delete(leaseKey, nil); err != nil {
			return err
		}
		if task.IdempotencyKey != "" {
			idemKey, _ := IdemKey(task.Type, task.IdempotencyKey)
			if err := batch.Delete(idemKey, nil); err != nil {
				return err
			}
		}
		return batch.Set(doneKey, raw, nil)
	})
}

func (q *Queue) failLocked(id flake.Id, task *pb.Task, nowMS int64, failure string) (FailResult, error) {
	result := FailResult{}
	err := q.db.ApplyBatch(func(batch *pebble.Batch) error {
		if err := q.stageFailedTask(batch, id, task, nowMS, failure); err != nil {
			return err
		}
		if task.Attempts >= task.MaxAttempts {
			result.Outcome = pb.FailTaskOutcome_FAIL_TASK_OUTCOME_DEAD
		} else {
			result.Outcome = pb.FailTaskOutcome_FAIL_TASK_OUTCOME_RETRY
			result.NextRunAt = task.RunAtMs
		}
		return nil
	})
	if err != nil {
		return FailResult{}, err
	}
	return result, nil
}

func (q *Queue) stageFailedTask(batch *pebble.Batch, id flake.Id, task *pb.Task, nowMS int64, failure string) error {
	oldLease, err := LeaseKey(task.LeaseUntilMs, id)
	if err != nil {
		return err
	}
	if err := batch.Delete(oldLease, nil); err != nil {
		return err
	}
	task.LastError = truncateError(failure)
	task.UpdatedAtMs = nowMS
	if task.Attempts >= task.MaxAttempts {
		completion := &pb.TaskCompletion{
			Task: cloneTask(task), Status: pb.TaskCompletionStatus_TASK_COMPLETION_STATUS_DEAD,
			LastError: task.LastError, FinishedAtMs: nowMS,
		}
		raw, err := proto.Marshal(completion)
		if err != nil {
			return err
		}
		taskKey, _ := TaskKey(id)
		if err := batch.Delete(taskKey, nil); err != nil {
			return err
		}
		if task.IdempotencyKey != "" {
			idemKey, _ := IdemKey(task.Type, task.IdempotencyKey)
			if err := batch.Delete(idemKey, nil); err != nil {
				return err
			}
		}
		doneKey, err := DoneKey(nowMS, id)
		if err != nil {
			return err
		}
		return batch.Set(doneKey, raw, nil)
	}
	definition, ok := q.registry.Definition(task.Type)
	if !ok {
		return fmt.Errorf("%w: Task %s has unregistered type", ErrCorrupt, task.Id)
	}
	delay := q.jitter(backoff(definition, task.Attempts))
	if delay < 0 {
		delay = 0
	}
	next := nowMS + delay.Milliseconds()
	if next < nowMS {
		return fmt.Errorf("%w: Task %s retry time overflow", ErrCorrupt, task.Id)
	}
	task.State = pb.TaskState_TASK_STATE_READY
	task.RunAtMs = next
	task.LeaseUntilMs = 0
	task.LeasedBy = ""
	task.ClaimedAtMs = 0
	readyKey, err := ReadyKey(task.Type, next, id)
	if err != nil {
		return err
	}
	if err := batch.Set(readyKey, nil, nil); err != nil {
		return err
	}
	return q.stageTask(batch, id, task)
}

func backoff(definition Definition, attempts uint32) time.Duration {
	delay := definition.BackoffBase
	for i := uint32(1); i < attempts && delay < definition.BackoffCap; i++ {
		if delay > definition.BackoffCap/2 {
			return definition.BackoffCap
		}
		delay *= 2
	}
	if delay > definition.BackoffCap {
		return definition.BackoffCap
	}
	return delay
}

type readyCandidate struct {
	key      store.Key
	taskType string
	runAtMS  int64
	id       flake.Id
	source   int
}

type readyHeap []readyCandidate

func (h readyHeap) Len() int { return len(h) }
func (h readyHeap) Less(i, j int) bool {
	if h[i].runAtMS != h[j].runAtMS {
		return h[i].runAtMS < h[j].runAtMS
	}
	return bytes.Compare(h[i].id[:], h[j].id[:]) < 0
}
func (h readyHeap) Swap(i, j int)   { h[i], h[j] = h[j], h[i] }
func (h *readyHeap) Push(value any) { *h = append(*h, value.(readyCandidate)) }
func (h *readyHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

func (q *Queue) readyCandidates(taskTypes []string, nowMS int64, maxTasks int) ([]readyCandidate, error) {
	types := make([]string, 0, len(taskTypes))
	seenTypes := make(map[string]struct{}, len(taskTypes))
	for _, taskType := range taskTypes {
		if _, ok := q.registry.Definition(taskType); !ok {
			return nil, fmt.Errorf("%w: unknown task type %q", ErrInvalidArgument, taskType)
		}
		if _, duplicate := seenTypes[taskType]; duplicate {
			continue
		}
		seenTypes[taskType] = struct{}{}
		types = append(types, taskType)
	}
	iters := make([]*store.Iterator, len(types))
	closeAll := func() error {
		var errs []error
		for _, iter := range iters {
			if iter != nil {
				errs = append(errs, iter.Error(), iter.Close())
			}
		}
		return errors.Join(errs...)
	}
	queue := make(readyHeap, 0, len(types))
	for i, taskType := range types {
		prefix, _ := ReadyTypePrefix(taskType)
		iter, err := q.db.NewIterator(prefix)
		if err != nil {
			_ = closeAll()
			return nil, err
		}
		iters[i] = iter
		if iter.First() {
			candidate, err := parseCandidate(iter.Key(), i)
			if err != nil {
				_ = closeAll()
				return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
			}
			if candidate.runAtMS <= nowMS {
				heap.Push(&queue, candidate)
			}
		}
	}
	candidates := make([]readyCandidate, 0, maxTasks)
	for queue.Len() > 0 && len(candidates) < maxTasks {
		candidate := heap.Pop(&queue).(readyCandidate)
		candidates = append(candidates, candidate)
		iter := iters[candidate.source]
		if iter.Next(); iter.Valid() {
			next, err := parseCandidate(iter.Key(), candidate.source)
			if err != nil {
				_ = closeAll()
				return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
			}
			if next.runAtMS <= nowMS {
				heap.Push(&queue, next)
			}
		}
	}
	if err := closeAll(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func parseCandidate(key store.Key, source int) (readyCandidate, error) {
	taskType, runAt, id, err := ParseReadyKey(key)
	if err != nil {
		return readyCandidate{}, err
	}
	return readyCandidate{key: key.Copy(), taskType: taskType, runAtMS: runAt, id: id, source: source}, nil
}

func validateWorker(workerID string) error {
	if len(workerID) == 0 || len(workerID) > MaxWorkerIDBytes {
		return fmt.Errorf("%w: worker_id length is outside 1..%d", ErrInvalidArgument, MaxWorkerIDBytes)
	}
	if !utf8.ValidString(workerID) {
		return fmt.Errorf("%w: worker_id must be valid UTF-8", ErrInvalidArgument)
	}
	for _, r := range workerID {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: worker_id contains control characters", ErrInvalidArgument)
		}
	}
	return nil
}

func truncateError(value string) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= MaxTaskErrorBytes {
		return value
	}
	value = value[:MaxTaskErrorBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func cloneTask(task *pb.Task) *pb.Task {
	if task == nil {
		return nil
	}
	return proto.Clone(task).(*pb.Task)
}
