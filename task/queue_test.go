package task

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

func newTestQueue(t *testing.T, definitions map[string]Definition) (*Queue, *store.Store, *testClock) {
	t.Helper()
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	registry, err := NewRegistry(definitions)
	require.NoError(t, err)
	clock := &testClock{now: time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)}
	queue, err := NewQueue(db, registry, Options{
		Now: clock.Now, Jitter: func(delay time.Duration) time.Duration { return delay },
	})
	require.NoError(t, err)
	return queue, db, clock
}

func countRows(t *testing.T, db *store.Store, prefix store.Key) int {
	t.Helper()
	count, err := db.ForwardScan(prefix, func(int, []byte, []byte) error { return nil })
	require.NoError(t, err)
	return count
}

func TestEnqueueActiveIdemAndAtomicBusinessWrite(t *testing.T) {
	queue, db, _ := newTestQueue(t, map[string]Definition{"rss.fetch": validDefinition()})
	ctx := context.Background()
	spec := Spec{Type: "rss.fetch", Payload: []byte("feed"), IdempotencyKey: "feed-1"}

	first, err := queue.Enqueue(ctx, spec)
	require.NoError(t, err)
	require.False(t, first.AlreadyExists)
	second, err := queue.Enqueue(ctx, spec)
	require.NoError(t, err)
	require.True(t, second.AlreadyExists)
	require.Equal(t, first.Task.Id, second.Task.Id)
	require.Equal(t, 1, countRows(t, db, model.Task.Prefix))
	require.Equal(t, 1, countRows(t, db, model.TaskReady.Prefix))
	require.Equal(t, 1, countRows(t, db, model.TaskIdem.Prefix))

	results, err := queue.EnqueueWith(ctx, []Spec{spec, spec}, func(batch *pebble.Batch) error {
		return batch.Set([]byte("business"), []byte("committed"), nil)
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.True(t, results[0].AlreadyExists)
	require.True(t, results[1].AlreadyExists)
	value, err := db.Get([]byte("business"))
	require.NoError(t, err)
	require.Equal(t, []byte("committed"), value)

	_, err = queue.EnqueueWith(ctx, []Spec{{Type: "rss.fetch", Payload: []byte("other")}}, func(batch *pebble.Batch) error {
		require.NoError(t, batch.Set([]byte("aborted"), []byte("value"), nil))
		return errors.New("abort")
	})
	require.ErrorContains(t, err, "abort")
	_, err = db.Get([]byte("aborted"))
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Equal(t, 1, countRows(t, db, model.Task.Prefix))
}

func TestClaimMergesTypesByDueTime(t *testing.T) {
	definitions := map[string]Definition{"rss.fetch": validDefinition(), "twitter.crawl": validDefinition()}
	queue, _, clock := newTestQueue(t, definitions)
	base := clock.Now().UnixMilli()
	for _, spec := range []Spec{
		{Type: "rss.fetch", Payload: []byte("later"), RunAtMS: base + 20},
		{Type: "twitter.crawl", Payload: []byte("first"), RunAtMS: base + 10},
		{Type: "rss.fetch", Payload: []byte("future"), RunAtMS: base + 1000},
	} {
		_, err := queue.Enqueue(context.Background(), spec)
		require.NoError(t, err)
	}
	clock.Set(clock.Now().Add(100 * time.Millisecond))
	claimed, err := queue.Claim(context.Background(), "worker", []string{"rss.fetch", "twitter.crawl"}, 3)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	require.Equal(t, []byte("first"), claimed[0].Payload)
	require.Equal(t, []byte("later"), claimed[1].Payload)
	for _, task := range claimed {
		require.Equal(t, pb.TaskState_TASK_STATE_INFLIGHT, task.State)
		require.EqualValues(t, 1, task.Attempts)
		require.EqualValues(t, 1, task.LeaseEpoch)
		require.Equal(t, "worker", task.LeasedBy)
	}
}

func TestConcurrentClaimReturnsTaskOnce(t *testing.T) {
	queue, _, _ := newTestQueue(t, map[string]Definition{"rss.fetch": validDefinition()})
	_, err := queue.Enqueue(context.Background(), Spec{Type: "rss.fetch"})
	require.NoError(t, err)

	const workers = 8
	results := make(chan []*pb.Task, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			claimed, err := queue.Claim(context.Background(), string(rune('a'+worker)), []string{"rss.fetch"}, 1)
			results <- claimed
			errs <- err
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	claimedCount := 0
	for result := range results {
		claimedCount += len(result)
	}
	require.Equal(t, 1, claimedCount)
}

func TestCompleteChecksFencingAndReleasesIdem(t *testing.T) {
	queue, db, _ := newTestQueue(t, map[string]Definition{"rss.fetch": validDefinition()})
	spec := Spec{Type: "rss.fetch", IdempotencyKey: "feed"}
	created, err := queue.Enqueue(context.Background(), spec)
	require.NoError(t, err)
	claimed, err := queue.Claim(context.Background(), "worker", []string{"rss.fetch"}, 1)
	require.NoError(t, err)

	err = queue.Complete(context.Background(), "worker", claimed[0].Id, claimed[0].LeaseEpoch+1)
	require.ErrorIs(t, err, ErrFailedPrecondition)
	err = queue.Complete(context.Background(), "worker", claimed[0].Id, claimed[0].LeaseEpoch)
	require.NoError(t, err)
	require.Zero(t, countRows(t, db, model.Task.Prefix))
	require.Zero(t, countRows(t, db, model.TaskLease.Prefix))
	require.Zero(t, countRows(t, db, model.TaskIdem.Prefix))
	require.Equal(t, 1, countRows(t, db, model.TaskDone.Prefix))

	again, err := queue.Enqueue(context.Background(), spec)
	require.NoError(t, err)
	require.NotEqual(t, created.Task.Id, again.Task.Id)
}

func TestFailRetriesThenMovesToDead(t *testing.T) {
	definition := validDefinition()
	definition.MaxAttempts = 2
	definition.BackoffBase = 10 * time.Second
	definition.BackoffCap = time.Minute
	queue, db, clock := newTestQueue(t, map[string]Definition{"rss.fetch": definition})
	created, err := queue.Enqueue(context.Background(), Spec{Type: "rss.fetch", IdempotencyKey: "feed"})
	require.NoError(t, err)

	claimed, err := queue.Claim(context.Background(), "worker", []string{"rss.fetch"}, 1)
	require.NoError(t, err)
	failedAt := clock.Now()
	result, err := queue.Fail(context.Background(), "worker", claimed[0].Id, claimed[0].LeaseEpoch, "temporary")
	require.NoError(t, err)
	require.Equal(t, pb.FailTaskOutcome_FAIL_TASK_OUTCOME_RETRY, result.Outcome)
	require.Equal(t, failedAt.Add(10*time.Second).UnixMilli(), result.NextRunAt)

	clock.Set(failedAt.Add(10 * time.Second))
	claimed, err = queue.Claim(context.Background(), "worker", []string{"rss.fetch"}, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, claimed[0].Attempts)
	result, err = queue.Fail(context.Background(), "worker", claimed[0].Id, claimed[0].LeaseEpoch, "permanent")
	require.NoError(t, err)
	require.Equal(t, pb.FailTaskOutcome_FAIL_TASK_OUTCOME_DEAD, result.Outcome)
	require.Zero(t, result.NextRunAt)
	require.Zero(t, countRows(t, db, model.Task.Prefix))
	require.Zero(t, countRows(t, db, model.TaskIdem.Prefix))

	var completion pb.TaskCompletion
	_, err = db.ForwardScan(model.TaskDone.Prefix, func(_ int, _, value []byte) error {
		return proto.Unmarshal(value, &completion)
	})
	require.NoError(t, err)
	require.Equal(t, pb.TaskCompletionStatus_TASK_COMPLETION_STATUS_DEAD, completion.Status)
	require.Equal(t, created.Task.Id, completion.Task.Id)
	require.Equal(t, "permanent", completion.LastError)
}

func TestRenewHasFixedHardLimitAndRejectsExpiredLease(t *testing.T) {
	definition := validDefinition()
	definition.LeaseDuration = time.Minute
	definition.MaxLease = 3 * time.Minute
	queue, _, clock := newTestQueue(t, map[string]Definition{"rss.fetch": definition})
	_, err := queue.Enqueue(context.Background(), Spec{Type: "rss.fetch"})
	require.NoError(t, err)
	claimed, err := queue.Claim(context.Background(), "worker", []string{"rss.fetch"}, 1)
	require.NoError(t, err)
	start := clock.Now()

	clock.Set(start.Add(30 * time.Second))
	renewed, err := queue.Renew(context.Background(), "worker", claimed[0].Id, claimed[0].LeaseEpoch)
	require.NoError(t, err)
	require.Equal(t, start.Add(90*time.Second).UnixMilli(), renewed.LeaseUntilMs)
	clock.Set(start.Add(89 * time.Second))
	renewed, err = queue.Renew(context.Background(), "worker", claimed[0].Id, claimed[0].LeaseEpoch)
	require.NoError(t, err)
	require.Equal(t, start.Add(149*time.Second).UnixMilli(), renewed.LeaseUntilMs)
	clock.Set(start.Add(148 * time.Second))
	renewed, err = queue.Renew(context.Background(), "worker", claimed[0].Id, claimed[0].LeaseEpoch)
	require.NoError(t, err)
	require.Equal(t, start.Add(3*time.Minute).UnixMilli(), renewed.LeaseUntilMs)
	clock.Set(start.Add(149 * time.Second))
	_, err = queue.Renew(context.Background(), "worker", claimed[0].Id, claimed[0].LeaseEpoch)
	require.ErrorIs(t, err, ErrFailedPrecondition)

	_, err = queue.Enqueue(context.Background(), Spec{Type: "rss.fetch"})
	require.NoError(t, err)
	second, err := queue.Claim(context.Background(), "second", []string{"rss.fetch"}, 1)
	require.NoError(t, err)
	clock.Set(time.UnixMilli(second[0].LeaseUntilMs))
	_, err = queue.Renew(context.Background(), "second", second[0].Id, second[0].LeaseEpoch)
	require.ErrorIs(t, err, ErrFailedPrecondition)
}

func TestReapRetriesThenDeadLettersExpiredTask(t *testing.T) {
	definition := validDefinition()
	definition.MaxAttempts = 2
	definition.LeaseDuration = time.Minute
	definition.BackoffBase = time.Second
	queue, db, clock := newTestQueue(t, map[string]Definition{"rss.fetch": definition})
	_, err := queue.Enqueue(context.Background(), Spec{Type: "rss.fetch"})
	require.NoError(t, err)
	claimed, err := queue.Claim(context.Background(), "worker", []string{"rss.fetch"}, 1)
	require.NoError(t, err)

	clock.Set(time.UnixMilli(claimed[0].LeaseUntilMs))
	reaped, err := queue.ReapOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, reaped)
	clock.Set(clock.Now().Add(time.Second))
	claimed, err = queue.Claim(context.Background(), "worker", []string{"rss.fetch"}, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	clock.Set(time.UnixMilli(claimed[0].LeaseUntilMs))
	reaped, err = queue.ReapOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, reaped)
	require.Zero(t, countRows(t, db, model.Task.Prefix))
	require.Equal(t, 1, countRows(t, db, model.TaskDone.Prefix))
}
