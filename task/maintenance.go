package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/store/flake"
	"google.golang.org/protobuf/proto"
)

type ListedTask struct {
	Task       *pb.Task
	Completion *pb.TaskCompletion
}

// List returns at most limit records in persisted index order. mode is one of
// ready, inflight, or dead. It never retains more than limit records.
func List(db *store.Store, mode string, limit int) ([]ListedTask, error) {
	if limit <= 0 || limit > 1000 {
		return nil, fmt.Errorf("%w: limit must be within 1..1000", ErrInvalidArgument)
	}
	result := make([]ListedTask, 0, limit)
	stop := func() error { return &store.Error{Code: store.StopIteration, Msg: "task list limit"} }
	switch mode {
	case "ready":
		err := model.TaskReady.Iter(db, func(key, _ []byte) error {
			_, _, id, err := ParseReadyKey(key)
			if err != nil {
				return err
			}
			task, err := loadTaskFromStore(db, id)
			if err != nil {
				return err
			}
			result = append(result, ListedTask{Task: task})
			if len(result) == limit {
				return stop()
			}
			return nil
		})
		return result, ignoreStop(err)
	case "inflight":
		err := model.TaskLease.Iter(db, func(key, _ []byte) error {
			_, id, err := ParseLeaseKey(key)
			if err != nil {
				return err
			}
			task, err := loadTaskFromStore(db, id)
			if err != nil {
				return err
			}
			result = append(result, ListedTask{Task: task})
			if len(result) == limit {
				return stop()
			}
			return nil
		})
		return result, ignoreStop(err)
	case "dead":
		err := model.TaskDone.Iter(db, func(_, raw []byte) error {
			completion := new(pb.TaskCompletion)
			if err := proto.Unmarshal(raw, completion); err != nil {
				return err
			}
			if completion.Status != pb.TaskCompletionStatus_TASK_COMPLETION_STATUS_DEAD {
				return nil
			}
			result = append(result, ListedTask{Task: completion.Task, Completion: completion})
			if len(result) == limit {
				return stop()
			}
			return nil
		})
		return result, ignoreStop(err)
	default:
		return nil, fmt.Errorf("%w: mode must be ready, inflight, or dead", ErrInvalidArgument)
	}
}

func Inspect(db *store.Store, taskID string) (ListedTask, error) {
	id, err := DecodeTaskID(taskID)
	if err != nil {
		return ListedTask{}, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	if task, err := loadTaskFromStore(db, id); err == nil {
		return ListedTask{Task: task}, nil
	} else if !errors.Is(err, ErrNotFound) {
		return ListedTask{}, err
	}
	var found *pb.TaskCompletion
	err = model.TaskDone.Iter(db, func(key, raw []byte) error {
		_, doneID, err := ParseDoneKey(key)
		if err != nil {
			return err
		}
		if doneID != id {
			return nil
		}
		found = new(pb.TaskCompletion)
		if err := proto.Unmarshal(raw, found); err != nil {
			return err
		}
		return &store.Error{Code: store.StopIteration, Msg: "task found"}
	})
	if err = ignoreStop(err); err != nil {
		return ListedTask{}, err
	}
	if found == nil {
		return ListedTask{}, ErrNotFound
	}
	return ListedTask{Task: found.Task, Completion: found}, nil
}

func (q *Queue) ReplayDead(ctx context.Context, taskID string) (EnqueueResult, error) {
	record, err := Inspect(q.db, taskID)
	if err != nil {
		return EnqueueResult{}, err
	}
	if record.Completion == nil || record.Completion.Status != pb.TaskCompletionStatus_TASK_COMPLETION_STATUS_DEAD || record.Task == nil {
		return EnqueueResult{}, fmt.Errorf("%w: Task %s is not dead", ErrFailedPrecondition, taskID)
	}
	return q.Enqueue(ctx, Spec{Type: record.Task.Type, Payload: record.Task.Payload, PayloadVersion: record.Task.PayloadVersion, IdempotencyKey: record.Task.IdempotencyKey})
}

// PurgeDone deletes completion history strictly before cutoff. dryRun performs
// the same bounded scan but never writes.
func PurgeDone(db *store.Store, cutoff time.Time, dryRun bool) (int, error) {
	cutoffMS := cutoff.UTC().UnixMilli()
	if cutoffMS < 0 {
		return 0, fmt.Errorf("%w: cutoff is before Unix epoch", ErrInvalidArgument)
	}
	count := 0
	err := model.TaskDone.Iter(db, func(key, _ []byte) error {
		finished, _, err := ParseDoneKey(key)
		if err != nil {
			return err
		}
		if finished >= cutoffMS {
			return &store.Error{Code: store.StopIteration, Msg: "cutoff reached"}
		}
		count++
		return nil
	})
	err = ignoreStop(err)
	if err != nil || dryRun || count == 0 {
		return count, err
	}
	timeBytes, _ := encodeTime(cutoffMS)
	end := model.NewKeyFrom(model.TaskDone.Prefix, timeBytes)
	err = db.ApplyBatch(func(batch *pebble.Batch) error { return batch.DeleteRange(model.TaskDone.Prefix, end, nil) })
	return count, err
}

func loadTaskFromStore(db *store.Store, id flake.Id) (*pb.Task, error) {
	key, err := TaskKey(id)
	if err != nil {
		return nil, err
	}
	raw, err := db.Get(key)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	task := new(pb.Task)
	if err := proto.Unmarshal(raw, task); err != nil {
		return nil, fmt.Errorf("%w: decode Task: %v", ErrCorrupt, err)
	}
	return task, nil
}

func ignoreStop(err error) error {
	var scanErr *store.Error
	if errors.As(err, &scanErr) && scanErr.Code == store.StopIteration {
		return nil
	}
	return err
}
