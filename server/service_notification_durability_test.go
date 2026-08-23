package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
)

func TestPersistDeadServiceFailureIsTaskIdempotent(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	user := createServiceUser(t, srv, "durable-failure-owner")
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind,
		Url: "https://example.com/durable-failure",
	})
	require.NoError(t, err)
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	state, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	state.Status = model.ServiceStatusDead
	state.DeadAtMs = now.Add(8 * 24 * time.Hour).UnixMilli()
	state.NextFetchMs = 0

	require.NoError(t, srv.persistDeadServiceFailure(context.Background(), serviceID, state))
	require.NoError(t, srv.persistDeadServiceFailure(context.Background(), serviceID, state))

	records, err := taskqueue.List(srv.rdb, "ready", 100)
	require.NoError(t, err)
	count := 0
	for _, record := range records {
		if record.Task != nil && record.Task.Type == notificationFeedServiceFailedTaskType {
			count++
		}
	}
	require.Equal(t, 1, count)
	persisted, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, model.ServiceStatusDead, persisted.Status)
	require.Equal(t, state.DeadAtMs, persisted.DeadAtMs)
}

func TestPersistDeadServiceFailureDoesNotCommitWithoutDurableTask(t *testing.T) {
	srv := newServiceServer(t)
	now := time.Date(2026, 8, 20, 13, 0, 0, 0, time.UTC)
	srv.rssNow = func() time.Time { return now }
	user := createServiceUser(t, srv, "closed-queue-owner")
	binding, err := srv.AddFeedService(context.Background(), &pb.AddFeedServiceRequest{
		ActorUuid: user.String(), TargetFeedUuid: user.String(), Kind: model.WebFeedServiceKind,
		Url: "https://example.com/closed-queue",
	})
	require.NoError(t, err)
	serviceID := uuid.Must(uuid.FromString(binding.ServiceUuid))
	state, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	state.Status = model.ServiceStatusDead
	state.DeadAtMs = now.Add(8 * 24 * time.Hour).UnixMilli()
	state.NextFetchMs = 0

	srv.tasks.StopAccepting()
	err = srv.persistDeadServiceFailure(context.Background(), serviceID, state)
	require.Error(t, err)
	require.True(t, errors.Is(err, taskqueue.ErrClosed))

	persisted, err := model.GetServiceState(srv.rdb, serviceID)
	require.NoError(t, err)
	require.Equal(t, model.ServiceStatusActive, persisted.Status)
	require.Zero(t, persisted.DeadAtMs)
}
