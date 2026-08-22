package server

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
)

func TestEventsHubRoutesByViewer(t *testing.T) {
	hub := newEventsHub()
	alice := uuid.Must(uuid.NewV4())
	bob := uuid.Must(uuid.NewV4())
	aliceConn, err := hub.register(alice)
	require.NoError(t, err)
	bobConn, err := hub.register(bob)
	require.NoError(t, err)

	hub.publish(alice)
	select {
	case <-aliceConn.events:
	default:
		t.Fatal("alice did not receive her dirty hint")
	}
	select {
	case <-bobConn.events:
		t.Fatal("bob received alice's dirty hint")
	default:
	}
}

func TestEventsHubEnforcesPerViewerLimit(t *testing.T) {
	hub := newEventsHub()
	viewer := uuid.Must(uuid.NewV4())
	for i := 0; i < maxSSEConnectionsPerViewer; i++ {
		_, err := hub.register(viewer)
		require.NoError(t, err)
	}
	_, err := hub.register(viewer)
	require.ErrorIs(t, err, errSSEViewerLimit)
}

func TestEventsHubEnforcesGlobalLimit(t *testing.T) {
	hub := newEventsHub()
	// Use one viewer per connection to avoid hitting the per-viewer limit first.
	for i := 0; i < maxSSEConnections; i++ {
		_, err := hub.register(uuid.Must(uuid.NewV4()))
		require.NoError(t, err)
	}
	_, err := hub.register(uuid.Must(uuid.NewV4()))
	require.ErrorIs(t, err, errSSEConnectionLimit)
}

func TestEventsHubDisconnectsSlowConsumer(t *testing.T) {
	hub := newEventsHub()
	viewer := uuid.Must(uuid.NewV4())
	conn, err := hub.register(viewer)
	require.NoError(t, err)
	for i := 0; i < cap(conn.events); i++ {
		conn.events <- struct{}{}
	}

	hub.publish(viewer)
	select {
	case <-conn.done:
	default:
		t.Fatal("slow consumer was not disconnected")
	}

	// The slow connection was removed, so a replacement can register.
	_, err = hub.register(viewer)
	require.NoError(t, err)
}

func TestEventsHubCloseWakesConnectionsAndRejectsNewOnes(t *testing.T) {
	hub := newEventsHub()
	viewer := uuid.Must(uuid.NewV4())
	conn, err := hub.register(viewer)
	require.NoError(t, err)
	hub.closeAll()
	hub.closeAll()

	select {
	case <-conn.done:
	default:
		t.Fatal("closeAll did not wake connection")
	}
	_, err = hub.register(viewer)
	require.ErrorIs(t, err, errSSEHubClosed)
}
