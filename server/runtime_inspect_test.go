package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func newRuntimeInspectTestDB(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db
}

func TestCollectRuntimeReportIsBoundedSnapshot(t *testing.T) {
	db := newRuntimeInspectTestDB(t)
	bus := newRealtimeBus()
	sub, err := bus.subscribe("test")
	require.NoError(t, err)
	defer bus.unsubscribe(sub)
	bus.drops.Store(7)

	report, err := collectRuntimeReport(db, bus)
	require.NoError(t, err)
	require.NotEmpty(t, report.CollectedAt)
	require.Positive(t, report.Process.RSSBytes)
	require.Positive(t, report.Process.OpenFDs)
	require.Positive(t, report.Go.HeapSysBytes)
	require.Positive(t, report.Go.Goroutines)
	require.Equal(t, int64(512<<20), report.Pebble.BlockCacheLimitBytes)
	require.Equal(t, 1, report.Realtime.Subscribers)
	require.Equal(t, uint64(7), report.Realtime.DroppedHints)
}

func TestRuntimeInspectCommandReturnsJSON(t *testing.T) {
	db := newRuntimeInspectTestDB(t)
	srv := &ApiServer{rdb: db, realtime: newRealtimeBus()}

	response, err := srv.Command(context.Background(), &pb.CommandRequest{Command: "RuntimeInspect"})
	require.NoError(t, err)
	require.Equal(t, "RuntimeInspect", response.Command)

	var report RuntimeReport
	require.NoError(t, json.Unmarshal([]byte(response.Result), &report))
	require.Positive(t, report.Process.RSSBytes)
	require.Equal(t, int64(512<<20), report.Pebble.BlockCacheLimitBytes)
}
