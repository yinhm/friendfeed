package server

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
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

func TestRealtimeObserverExcludesInitiatingActor(t *testing.T) {
	api := newServiceServer(t)
	sub, err := api.realtimeBus().subscribe("observer-test")
	require.NoError(t, err)
	actor := uuid.Must(uuid.NewV4())
	follower := uuid.Must(uuid.NewV4())
	entry := uuid.Must(uuid.NewV4())
	observer := api.realtimeObserverExcluding(actor)

	observer(actor, entry, model.TimelineActivityPublish, time.Now().UTC())
	select {
	case <-sub.ch:
		t.Fatal("initiating actor received its own realtime hint")
	default:
	}

	observer(follower, entry, model.TimelineActivityPublish, time.Now().UTC())
	require.Equal(t, follower, (<-sub.ch).Viewer)
}

func TestBeginShutdownWakesRealtimeStreamBeforeGracefulStop(t *testing.T) {
	api := newServiceServer(t)
	listener := bufconn.Listen(1024 * 1024)
	rpcServer := grpc.NewServer()
	pb.RegisterRealtimeServer(rpcServer, api)
	serveDone := make(chan error, 1)
	go func() { serveDone <- rpcServer.Serve(listener) }()
	t.Cleanup(func() {
		rpcServer.Stop()
		_ = listener.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)
	require.NoError(t, err)
	defer conn.Close()
	stream, err := pb.NewRealtimeClient(conn).SubscribeRealtimeEvents(ctx,
		&pb.SubscribeRealtimeEventsRequest{SubscriberId: "shutdown-test"})
	require.NoError(t, err)

	bus := api.realtimeBus()
	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.subscribers) == 1
	}, time.Second, time.Millisecond)

	api.BeginShutdown()
	gracefulDone := make(chan struct{})
	go func() {
		rpcServer.GracefulStop()
		close(gracefulDone)
	}()

	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
	select {
	case <-gracefulDone:
	case <-time.After(time.Second):
		t.Fatal("GracefulStop remained blocked by the permanent realtime stream")
	}
	require.NoError(t, <-serveDone)
}

func TestShutdownStopsRealtimeBeforeWaitingForStreams(t *testing.T) {
	api := newServiceServer(t)
	bus := api.realtimeBus()
	api.wg.Add(1)
	go func() {
		defer api.wg.Done()
		<-bus.done
	}()

	shutdownDone := make(chan struct{})
	go func() {
		api.Shutdown()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("Shutdown waited for realtime streams before stopping the broadcaster")
	}
}
