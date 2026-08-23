package server

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
)

func TestEventsHubRoutesByViewer(t *testing.T) {
	hub := newEventsHub()
	alice := uuid.Must(uuid.NewV4())
	bob := uuid.Must(uuid.NewV4())
	aliceConn, err := hub.register(alice)
	require.NoError(t, err)
	bobConn, err := hub.register(bob)
	require.NoError(t, err)

	hub.publish(alice, pb.RealtimeEventType_REALTIME_EVENT_TIMELINE_DIRTY)
	select {
	case eventType := <-aliceConn.events:
		require.Equal(t, pb.RealtimeEventType_REALTIME_EVENT_TIMELINE_DIRTY, eventType)
	default:
		t.Fatal("alice did not receive her dirty hint")
	}
	select {
	case <-bobConn.events:
		t.Fatal("bob received alice's dirty hint")
	default:
	}
	hub.publish(alice, pb.RealtimeEventType_REALTIME_EVENT_NOTIFICATIONS_DIRTY)
	require.Equal(t, pb.RealtimeEventType_REALTIME_EVENT_NOTIFICATIONS_DIRTY, <-aliceConn.events)
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
		conn.events <- pb.RealtimeEventType_REALTIME_EVENT_TIMELINE_DIRTY
	}

	hub.publish(viewer, pb.RealtimeEventType_REALTIME_EVENT_TIMELINE_DIRTY)
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

func TestEventsHandlerRequiresLoginAndSetsStreamingHeaders(t *testing.T) {
	server := newGroupTestServer(new(fakeGroupClient))
	state := &webRealtime{hub: newEventsHub()}
	webRealtimeStates.Store(server, state)
	t.Cleanup(func() {
		state.hub.closeAll()
		webRealtimeStates.Delete(server)
	})
	router := groupTestRouter(server)
	router.GET("/a/events", LoginRequired(), server.EventsHandler)
	httpServer := httptest.NewServer(router)
	defer httpServer.Close()

	unauthorized, err := http.NewRequest(http.MethodGet, httpServer.URL+"/a/events", nil)
	require.NoError(t, err)
	unauthorized.Header.Set("X-Requested-With", "XMLHttpRequest")
	response, err := http.DefaultClient.Do(unauthorized)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)

	cookie := groupLoginCookie(t, router)
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/a/events", nil)
	require.NoError(t, err)
	request.AddCookie(cookie)
	response, err = http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))
	require.Equal(t, "no-cache", response.Header.Get("Cache-Control"))
	require.Equal(t, "no", response.Header.Get("X-Accel-Buffering"))

	reader := bufio.NewReader(response.Body)
	var initial strings.Builder
	for !strings.Contains(initial.String(), ":ok\n\n") {
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		initial.WriteString(line)
	}
	require.Contains(t, initial.String(), "retry: 3000\n\n")
	cancel()
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
}
