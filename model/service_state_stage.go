package model

import (
	"errors"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
)

// StagePutServiceState writes ServiceState into the caller's atomic batch.
// It uses the existing table/value encoding and exists only so a lifecycle
// transition can be committed with its durable follow-up task.
func StagePutServiceState(batch *pebble.Batch, serviceID uuid.UUID, state *pb.ServiceState) error {
	if batch == nil {
		return errors.New("ServiceState batch is required")
	}
	if serviceID == uuid.Nil || state == nil || state.ServiceUuid != serviceID.String() {
		return errors.New("ServiceState identity mismatch")
	}
	return setProto(batch, ServiceState.PrefixAppend(serviceID.Bytes()), state)
}
