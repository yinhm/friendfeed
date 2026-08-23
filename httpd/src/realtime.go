package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
)

const (
	maxSSEConnectionsPerViewer = 3
	maxSSEConnections          = 512
	sseConnectionBuffer        = 32
	sseHeartbeatInterval       = 25 * time.Second
	realtimeReconnectMin       = time.Second
	realtimeReconnectMax       = 30 * time.Second
)

var (
	errSSEHubClosed       = errors.New("realtime hub is closed")
	errSSEViewerLimit     = errors.New("too many realtime connections for viewer")
	errSSEConnectionLimit = errors.New("too many realtime connections")
)

type sseConnection struct {
	events   chan pb.RealtimeEventType
	done     chan struct{}
	stopOnce sync.Once
}

func newSSEConnection() *sseConnection {
	return &sseConnection{
		events: make(chan pb.RealtimeEventType, sseConnectionBuffer),
		done:   make(chan struct{}),
	}
}

func (c *sseConnection) stop() {
	c.stopOnce.Do(func() { close(c.done) })
}

type eventsHub struct {
	mu               sync.Mutex
	viewers          map[uuid.UUID]map[*sseConnection]struct{}
	totalConnections int
	closed           bool
}

func newEventsHub() *eventsHub {
	return &eventsHub{viewers: make(map[uuid.UUID]map[*sseConnection]struct{})}
}

func (h *eventsHub) register(viewer uuid.UUID) (*sseConnection, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errSSEHubClosed
	}
	connections := h.viewers[viewer]
	if len(connections) >= maxSSEConnectionsPerViewer {
		return nil, errSSEViewerLimit
	}
	if h.totalConnections >= maxSSEConnections {
		return nil, errSSEConnectionLimit
	}
	if connections == nil {
		connections = make(map[*sseConnection]struct{})
		h.viewers[viewer] = connections
	}
	conn := newSSEConnection()
	connections[conn] = struct{}{}
	h.totalConnections++
	return conn, nil
}

func (h *eventsHub) unregister(viewer uuid.UUID, conn *sseConnection) {
	if conn == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	connections := h.viewers[viewer]
	if _, ok := connections[conn]; !ok {
		return
	}
	delete(connections, conn)
	h.totalConnections--
	if len(connections) == 0 {
		delete(h.viewers, viewer)
	}
}

func (h *eventsHub) publish(viewer uuid.UUID, eventType pb.RealtimeEventType) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	connections := h.viewers[viewer]
	for conn := range connections {
		select {
		case conn.events <- eventType:
		default:
			// A slow tab is cheaper to reconnect than to let it grow memory or
			// apply backpressure to the single ffdb receive loop.
			conn.stop()
			delete(connections, conn)
			h.totalConnections--
		}
	}
	if len(connections) == 0 {
		delete(h.viewers, viewer)
	}
}

func (h *eventsHub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for _, connections := range h.viewers {
		for conn := range connections {
			conn.stop()
		}
	}
	h.viewers = make(map[uuid.UUID]map[*sseConnection]struct{})
	h.totalConnections = 0
}

type webRealtime struct {
	hub          *eventsHub
	client       pb.RealtimeClient
	subscriberID string
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

var webRealtimeStates sync.Map // map[*Server]*webRealtime

// StartRealtime starts exactly one ffdb server-stream subscription for this
// ffweb process. Initial ffdb unavailability is not fatal: the receive loop
// reconnects until ShutdownRealtime cancels it.
func (s *Server) StartRealtime(conn grpc.ClientConnInterface) {
	ctx, cancel := context.WithCancel(context.Background())
	state := &webRealtime{
		hub:          newEventsHub(),
		client:       pb.NewRealtimeClient(conn),
		subscriberID: "ffweb-" + s.worker.Id,
		ctx:          ctx,
		cancel:       cancel,
	}
	actual, loaded := webRealtimeStates.LoadOrStore(s, state)
	if loaded {
		cancel()
		_ = actual
		return
	}
	state.wg.Add(1)
	go state.receiveLoop()
}

func realtimeState(s *Server) *webRealtime {
	state, ok := webRealtimeStates.Load(s)
	if !ok {
		return nil
	}
	return state.(*webRealtime)
}

func (s *Server) ShutdownRealtime() {
	state, ok := webRealtimeStates.LoadAndDelete(s)
	if !ok {
		return
	}
	rt := state.(*webRealtime)
	rt.cancel()
	rt.hub.closeAll()
	rt.wg.Wait()
}

func (r *webRealtime) receiveLoop() {
	defer r.wg.Done()
	backoff := realtimeReconnectMin
	for {
		if r.ctx.Err() != nil {
			return
		}
		stream, err := r.client.SubscribeRealtimeEvents(r.ctx, &pb.SubscribeRealtimeEventsRequest{
			SubscriberId: r.subscriberID,
		})
		if err != nil {
			if !r.waitReconnect(backoff) {
				return
			}
			backoff = nextRealtimeBackoff(backoff)
			continue
		}

		connected := false
		for {
			event, recvErr := stream.Recv()
			if recvErr != nil {
				if r.ctx.Err() != nil || errors.Is(recvErr, context.Canceled) {
					return
				}
				if recvErr != io.EOF {
					slog.Warn("realtime stream disconnected", "err", recvErr)
				}
				break
			}
			connected = true
			backoff = realtimeReconnectMin
			eventType := event.GetType()
			if eventType != pb.RealtimeEventType_REALTIME_EVENT_TIMELINE_DIRTY &&
				eventType != pb.RealtimeEventType_REALTIME_EVENT_NOTIFICATIONS_DIRTY {
				continue
			}
			viewer, parseErr := uuid.FromString(event.GetViewerUuid())
			if parseErr != nil || viewer == uuid.Nil {
				slog.Warn("realtime event has invalid viewer UUID")
				continue
			}
			r.hub.publish(viewer, eventType)
		}
		if connected {
			backoff = realtimeReconnectMin
		}
		if !r.waitReconnect(backoff) {
			return
		}
		backoff = nextRealtimeBackoff(backoff)
	}
}

func nextRealtimeBackoff(current time.Duration) time.Duration {
	if current >= realtimeReconnectMax/2 {
		return realtimeReconnectMax
	}
	return current * 2
}

func (r *webRealtime) waitReconnect(base time.Duration) bool {
	if base <= 0 {
		base = realtimeReconnectMin
	}
	// Add up to 20% positive jitter. Exact timing is not correctness state.
	jitterMax := base / 5
	jitter := time.Duration(0)
	if jitterMax > 0 {
		jitter = time.Duration(rand.Int64N(int64(jitterMax) + 1))
	}
	timer := time.NewTimer(base + jitter)
	defer timer.Stop()
	select {
	case <-r.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// EventsHandler exposes only a viewer-scoped dirty hint. The authenticated
// principal comes from the session; clients cannot subscribe to another UUID.
func (s *Server) EventsHandler(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	viewer, err := uuid.FromString(CurrentUserUuid(c))
	if err != nil || viewer == uuid.Nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	state := realtimeState(s)
	if state == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	conn, err := state.hub.register(viewer)
	if err != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	defer state.hub.unregister(viewer, conn)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	if _, err := fmt.Fprint(c.Writer, "retry: 3000\n\n:ok\n\n"); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-conn.done:
			return
		case eventType := <-conn.events:
			eventName := "timeline-dirty"
			if eventType == pb.RealtimeEventType_REALTIME_EVENT_NOTIFICATIONS_DIRTY {
				eventName = "notifications-dirty"
			}
			if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: {}\n\n", eventName); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(c.Writer, ":ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
