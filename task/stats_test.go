package task

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/store"
)

func TestCollectStats(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	registry, err := NewRegistry(map[string]Definition{"test": {
		MaxAttempts: 1, LeaseDuration: time.Minute, MaxLease: time.Minute,
		BackoffBase: time.Second, BackoffCap: time.Minute,
	}})
	require.NoError(t, err)
	queue, err := NewQueue(db, registry, Options{})
	require.NoError(t, err)
	now := time.Now().UTC()
	_, err = queue.Enqueue(context.Background(), Spec{Type: "test", PayloadVersion: 1, RunAtMS: now.Add(-time.Minute).UnixMilli()})
	require.NoError(t, err)

	stats, err := CollectStats(db, now)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.Ready)
	require.GreaterOrEqual(t, stats.OldestReadyAgeMS, int64(time.Minute/time.Millisecond))
	require.False(t, stats.Truncated)
}
