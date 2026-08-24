package task

import (
	"time"

	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

const StatsScanLimit = 100000

// Stats is a bounded operational summary. Truncated is set when a table has
// more than StatsScanLimit rows, so callers never mistake a partial count for
// an exact one.
type Stats struct {
	Ready            int64 `json:"ready"`
	Inflight         int64 `json:"inflight"`
	Dead             int64 `json:"dead"`
	OldestReadyAgeMS int64 `json:"oldest_ready_age_ms"`
	Truncated        bool  `json:"truncated"`
}

func CollectStats(db *store.Store, now time.Time) (Stats, error) {
	var result Stats
	stop := func() error { return &store.Error{Code: store.StopIteration, Msg: "task stats scan limit"} }
	readyScanned := 0
	err := model.TaskReady.Iter(db, func(key, _ []byte) error {
		if readyScanned == StatsScanLimit {
			result.Truncated = true
			return stop()
		}
		_, runAt, _, err := ParseReadyKey(key)
		if err != nil {
			return err
		}
		readyScanned++
		result.Ready++
		age := now.UTC().UnixMilli() - runAt
		if age > result.OldestReadyAgeMS {
			result.OldestReadyAgeMS = age
		}
		return nil
	})
	if err = ignoreStop(err); err != nil {
		return Stats{}, err
	}
	leaseScanned := 0
	err = model.TaskLease.Iter(db, func(_, _ []byte) error {
		if leaseScanned == StatsScanLimit {
			result.Truncated = true
			return stop()
		}
		leaseScanned++
		result.Inflight++
		return nil
	})
	if err = ignoreStop(err); err != nil {
		return Stats{}, err
	}
	doneScanned := 0
	err = model.TaskDone.Iter(db, func(_, raw []byte) error {
		if doneScanned == StatsScanLimit {
			result.Truncated = true
			return stop()
		}
		doneScanned++
		completion := new(pb.TaskCompletion)
		if err := proto.Unmarshal(raw, completion); err != nil {
			return err
		}
		if completion.Status == pb.TaskCompletionStatus_TASK_COMPLETION_STATUS_DEAD {
			result.Dead++
		}
		return nil
	})
	return result, ignoreStop(err)
}
