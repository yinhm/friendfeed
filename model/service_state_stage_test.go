package model

import (
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func TestStagePutServiceStateUsesExistingEncoding(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	serviceID := uuid.Must(uuid.NewV4())
	state := &pb.ServiceState{
		ServiceUuid: serviceID.String(),
		Status:      ServiceStatusDead,
		DeadAtMs:    12345,
	}
	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StagePutServiceState(batch, serviceID, state)
	}))
	got, err := GetServiceState(db, serviceID)
	require.NoError(t, err)
	require.True(t, proto.Equal(state, got))
}

func TestStagePutServiceStateRejectsMismatchedIdentity(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	serviceID := uuid.Must(uuid.NewV4())
	require.Error(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		return StagePutServiceState(batch, serviceID, &pb.ServiceState{ServiceUuid: uuid.Must(uuid.NewV4()).String()})
	}))
}
