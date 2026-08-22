package server

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
)

func TestRealtimeBusBroadcastsAndUnsubscribes(t *testing.T) {
	bus := newRealtimeBus()
	first, err := bus.subscribe("first")
	require.NoError(t, err)
	second, err := bus.subscribe("second")
	require.NoError(t, err)

	hint := realtimeHint{
		Viewer: uuid.Must(uuid.NewV4()),
		Object: uuid.Must(uuid.NewV4()),
		Kind:   model.TimelineActivityPublish,
		At:     time.Now().UTC(),
	}
	bus.publish(hint)
	require.Equal(t, hint, <-first.ch)
	require.Equal(t, hint, <-second.ch)

	bus.unsubscribe(first)
	bus.publish(hint)
	select {
	case <-first.ch:
		t.Fatal("unsubscribed consumer received an event")
	default:
	}
	require.Equal(t, hint, <-second.ch)
}

func TestRealtimeBusSlowSubscriberNeverBlocksOthers(t *testing.T) {
	bus := newRealtimeBus()
	slow, err := bus.subscribe("slow")
	require.NoError(t, err)
	fast, err := bus.subscribe("fast")
	require.NoError(t, err)

	// Fill only the slow channel. The next publish must be dropped for slow
	// while still reaching fast synchronously.
	for i := 0; i < cap(slow.ch); i++ {
		slow.ch <- realtimeHint{}
	}
	hint := realtimeHint{Viewer: uuid.Must(uuid.NewV4())}
	bus.publish(hint)
	require.Equal(t, hint, <-fast.ch)
	require.Equal(t, uint64(1), bus.dropCount())
}

func TestRealtimeBusStopWakesSubscribersAndRejectsNewOnes(t *testing.T) {
	bus := newRealtimeBus()
	_, err := bus.subscribe("existing")
	require.NoError(t, err)
	bus.stop()
	bus.stop() // idempotent

	select {
	case <-bus.done:
	default:
		t.Fatal("stop did not close done")
	}
	_, err = bus.subscribe("late")
	require.ErrorIs(t, err, errRealtimeStopped)
	bus.publish(realtimeHint{}) // drop silently after shutdown
}
