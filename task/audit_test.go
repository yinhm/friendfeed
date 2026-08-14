package task

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
)

func TestAuditTracksQueueStateAndDrift(t *testing.T) {
	queue, db, _ := newTestQueue(t, map[string]Definition{"rss.fetch": validDefinition()})
	_, err := queue.Enqueue(context.Background(), Spec{Type: "rss.fetch", Payload: []byte("one"), IdempotencyKey: "one"})
	require.NoError(t, err)
	_, err = queue.Enqueue(context.Background(), Spec{Type: "rss.fetch", Payload: []byte("two")})
	require.NoError(t, err)
	claimed, err := queue.Claim(context.Background(), "worker", []string{"rss.fetch"}, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	ready, err := queue.Enqueue(context.Background(), Spec{Type: "rss.fetch", Payload: []byte("three")})
	require.NoError(t, err)

	stats, err := Audit(db)
	require.NoError(t, err)
	require.Equal(t, 3, stats.Tasks)
	require.Equal(t, 2, stats.Ready)
	require.Equal(t, 1, stats.Leases)
	require.Equal(t, 1, stats.Idempotency)

	id, err := DecodeTaskID(ready.Task.Id)
	require.NoError(t, err)
	readyKey, err := ReadyKey(ready.Task.Type, ready.Task.RunAtMs, id)
	require.NoError(t, err)
	require.NoError(t, db.Delete(readyKey))
	stats, err = Audit(db)
	require.NoError(t, err)
	require.Equal(t, 1, stats.MissingReady)
	require.Equal(t, 1, countRows(t, db, model.TaskReady.Prefix))
}
