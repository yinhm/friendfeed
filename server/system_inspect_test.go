package server

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func TestCollectSystemReport(t *testing.T) {
	db := newRuntimeInspectTestDB(t)
	now := time.Now().UTC()
	active := uuid.Must(uuid.NewV4())
	dead := uuid.Must(uuid.NewV4())
	require.NoError(t, model.PutServiceState(db, active, &pb.ServiceState{
		ServiceUuid: active.String(), Status: model.ServiceStatusActive, NextFetchMs: now.Add(-time.Minute).UnixMilli(),
	}))
	require.NoError(t, model.PutServiceState(db, dead, &pb.ServiceState{
		ServiceUuid: dead.String(), Status: model.ServiceStatusDead,
	}))
	srv := &ApiServer{
		rdb: db, timelineBuildSlots: make(chan struct{}, 8),
		timelineRetryAfter: map[uuid.UUID]time.Time{active: now.Add(time.Minute)},
	}
	srv.timelineBuildSlots <- struct{}{}
	srv.notificationTrims.Store(2)
	srv.publicTimelineBumps.Store(9)

	report, err := srv.collectSystemReport(now)
	require.NoError(t, err)
	require.Equal(t, int64(1), report.Services.Active)
	require.Equal(t, int64(1), report.Services.Dead)
	require.Equal(t, int64(1), report.Services.Due)
	require.Equal(t, 1, report.Timeline.MaintenanceRunning)
	require.Equal(t, 8, report.Timeline.MaintenanceLimit)
	require.Equal(t, 1, report.Timeline.RetryBackoffs)
	require.Equal(t, int64(2), report.Notification.TrimsRunning)
	require.Equal(t, int64(9), report.Public.BumpsSinceTrim)
}
