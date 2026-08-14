package task

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/store/flake"
	"google.golang.org/protobuf/proto"
)

// AuditStats is deliberately counter-only: queue audit must remain bounded in
// memory even when the database contains millions of completed tasks.
type AuditStats struct {
	Tasks, Ready, Leases, Idempotency, Done int
	MissingReady, MissingLease, MissingIdem int
	OrphanReady, OrphanLease, OrphanIdem    int
	InvalidDone                             int
}

func Audit(db *store.Store) (AuditStats, error) {
	var stats AuditStats
	if err := model.Task.Iter(db, func(key, raw []byte) error {
		id, err := ParseTaskKey(key)
		if err != nil {
			return err
		}
		task := new(pb.Task)
		if err := proto.Unmarshal(raw, task); err != nil {
			return fmt.Errorf("decode Task %s: %w", EncodeTaskID(id), err)
		}
		if task.Id != EncodeTaskID(id) {
			return fmt.Errorf("Task key/id mismatch for %s", task.Id)
		}
		switch task.State {
		case pb.TaskState_TASK_STATE_READY:
			index, err := ReadyKey(task.Type, task.RunAtMs, id)
			if err != nil {
				return err
			}
			exists, err := db.Exists(index)
			if err != nil {
				return err
			}
			if !exists {
				stats.MissingReady++
			}
		case pb.TaskState_TASK_STATE_INFLIGHT:
			index, err := LeaseKey(task.LeaseUntilMs, id)
			if err != nil {
				return err
			}
			exists, err := db.Exists(index)
			if err != nil {
				return err
			}
			if !exists {
				stats.MissingLease++
			}
		default:
			return fmt.Errorf("Task %s has invalid state %s", task.Id, task.State)
		}
		if task.IdempotencyKey != "" {
			index, err := IdemKey(task.Type, task.IdempotencyKey)
			if err != nil {
				return err
			}
			value, err := db.Get(index)
			if errors.Is(err, store.ErrNotFound) {
				stats.MissingIdem++
			} else if err != nil {
				return err
			} else if !bytes.Equal(value, id[:]) {
				return fmt.Errorf("Task %s Idem points elsewhere", task.Id)
			}
		}
		stats.Tasks++
		return nil
	}); err != nil {
		return stats, err
	}

	if err := model.TaskReady.Iter(db, func(key, _ []byte) error {
		_, _, id, err := ParseReadyKey(key)
		if err != nil {
			return err
		}
		exists, err := taskExists(db, id)
		if err != nil {
			return err
		}
		if !exists {
			stats.OrphanReady++
		}
		stats.Ready++
		return nil
	}); err != nil {
		return stats, err
	}
	if err := model.TaskLease.Iter(db, func(key, _ []byte) error {
		_, id, err := ParseLeaseKey(key)
		if err != nil {
			return err
		}
		exists, err := taskExists(db, id)
		if err != nil {
			return err
		}
		if !exists {
			stats.OrphanLease++
		}
		stats.Leases++
		return nil
	}); err != nil {
		return stats, err
	}
	if err := model.TaskIdem.Iter(db, func(key, value []byte) error {
		if _, err := ParseIdemKey(key); err != nil {
			return err
		}
		if len(value) != TaskIDSize {
			return fmt.Errorf("invalid TaskIdem value length %d", len(value))
		}
		var id flake.Id
		copy(id[:], value)
		exists, err := taskExists(db, id)
		if err != nil {
			return err
		}
		if !exists {
			stats.OrphanIdem++
		}
		stats.Idempotency++
		return nil
	}); err != nil {
		return stats, err
	}
	if err := model.TaskDone.Iter(db, func(key, raw []byte) error {
		finished, id, err := ParseDoneKey(key)
		if err != nil {
			return err
		}
		completion := new(pb.TaskCompletion)
		if err := proto.Unmarshal(raw, completion); err != nil {
			return err
		}
		if completion.Task == nil || completion.Task.Id != EncodeTaskID(id) || completion.FinishedAtMs != finished ||
			(completion.Status != pb.TaskCompletionStatus_TASK_COMPLETION_STATUS_OK && completion.Status != pb.TaskCompletionStatus_TASK_COMPLETION_STATUS_DEAD) {
			stats.InvalidDone++
		}
		stats.Done++
		return nil
	}); err != nil {
		return stats, err
	}
	return stats, nil
}

func taskExists(db *store.Store, id flake.Id) (bool, error) {
	key, err := TaskKey(id)
	if err != nil {
		return false, err
	}
	return db.Exists(key)
}
