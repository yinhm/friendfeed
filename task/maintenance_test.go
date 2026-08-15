package task

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMaintenanceListInspectReplayAndPurge(t *testing.T) {
	definition := validDefinition()
	definition.MaxAttempts = 1
	queue, db, clock := newTestQueue(t, map[string]Definition{"rss.fetch": definition})

	result, err := queue.Enqueue(context.Background(), Spec{Type: "rss.fetch", Payload: []byte("payload"), PayloadVersion: 1, IdempotencyKey: "feed"})
	require.NoError(t, err)
	ready, err := List(db, "ready", 10)
	require.NoError(t, err)
	require.Len(t, ready, 1)
	require.Equal(t, result.Task.Id, ready[0].Task.Id)

	claimed, err := queue.Claim(context.Background(), "worker", []string{"rss.fetch"}, 1)
	require.NoError(t, err)
	_, err = queue.Fail(context.Background(), "worker", claimed[0].Id, claimed[0].LeaseEpoch, "permanent")
	require.NoError(t, err)
	dead, err := List(db, "dead", 10)
	require.NoError(t, err)
	require.Len(t, dead, 1)
	require.Equal(t, result.Task.Id, dead[0].Task.Id)
	inspected, err := Inspect(db, result.Task.Id)
	require.NoError(t, err)
	require.NotNil(t, inspected.Completion)

	replayed, err := queue.ReplayDead(context.Background(), result.Task.Id)
	require.NoError(t, err)
	require.NotEqual(t, result.Task.Id, replayed.Task.Id)
	require.Equal(t, []byte("payload"), replayed.Task.Payload)

	count, err := PurgeDone(db, clock.Now().Add(time.Millisecond), true)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	_, err = Inspect(db, result.Task.Id)
	require.NoError(t, err)
	count, err = PurgeDone(db, clock.Now().Add(time.Millisecond), false)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	_, err = Inspect(db, result.Task.Id)
	require.ErrorIs(t, err, ErrNotFound)
}
