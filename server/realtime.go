package server

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
)

const realtimeSubscriberBuffer = 64

var errRealtimeStopped = errors.New("realtime broadcaster stopped")

type realtimeHint struct {
	Viewer uuid.UUID
	Object uuid.UUID
	Kind   model.TimelineActivityKind
	At     time.Time
}

type realtimeSubscription struct {
	id   uint64
	name string
	ch   chan realtimeHint
}

type realtimeBus struct {
	mu          sync.Mutex
	subscribers map[uint64]*realtimeSubscription
	nextID      uint64
	stopped     bool
	done        chan struct{}
	drops       atomic.Uint64
}

func newRealtimeBus() *realtimeBus {
	return &realtimeBus{
		subscribers: make(map[uint64]*realtimeSubscription),
		done:        make(chan struct{}),
	}
}

func (b *realtimeBus) subscribe(name string) (*realtimeSubscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return nil, errRealtimeStopped
	}
	b.nextID++
	sub := &realtimeSubscription{
		id:   b.nextID,
		name: name,
		ch:   make(chan realtimeHint, realtimeSubscriberBuffer),
	}
	b.subscribers[sub.id] = sub
	return sub, nil
}

func (b *realtimeBus) unsubscribe(sub *realtimeSubscription) {
	if sub == nil {
		return
	}
	b.mu.Lock()
	delete(b.subscribers, sub.id)
	b.mu.Unlock()
}

// publish is deliberately best-effort. It never waits for a subscriber and
// never closes subscriber channels, so a slow/reconnecting ffweb cannot apply
// backpressure to a committed timeline move.
func (b *realtimeBus) publish(hint realtimeHint) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return
	}
	for _, sub := range b.subscribers {
		select {
		case sub.ch <- hint:
		default:
			b.drops.Add(1)
		}
	}
}

func (b *realtimeBus) stop() {
	b.mu.Lock()
	if !b.stopped {
		b.stopped = true
		close(b.done)
	}
	b.mu.Unlock()
}

func (b *realtimeBus) dropCount() uint64 {
	return b.drops.Load()
}

func (s *ApiServer) realtimeBus() *realtimeBus {
	return s.realtime
}

func (s *ApiServer) observeTimelineMove(viewer, entry uuid.UUID, kind model.TimelineActivityKind, at time.Time) {
	s.realtimeBus().publish(realtimeHint{
		Viewer: viewer,
		Object: entry,
		Kind:   kind,
		At:     at.UTC(),
	})
}

// BeginShutdown terminates permanent realtime streams before grpc.GracefulStop
// waits for RPC handlers. Full background-job draining and Pebble close remain
// in Shutdown after the gRPC server has drained.
func (s *ApiServer) BeginShutdown() {
	s.StopTaskClaims()
	if s.realtime != nil {
		s.realtime.stop()
	}
}
