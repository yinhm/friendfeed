package main

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// Health service names reported on the standard gRPC health checking
// service (grpc.health.v1.Health). Each name tracks the init state of
// one component; the empty name reports overall readiness and flips to
// SERVING only after every component has been marked ready.
const (
	healthServicePebble = "ffdb.Pebble"
	healthServiceSearch = "ffdb.Search"
	healthServiceAPI    = "ffdb.Api"
	healthServiceAll    = ""
)

// healthCheck wires the standard gRPC health server into startup and
// shutdown: every component starts NOT_SERVING and is marked SERVING
// once its init step completes, so a component that failed or has not
// finished initializing never reports ready.
type healthCheck struct {
	srv *health.Server
}

// newHealthCheck registers the health service on rpcServer with all
// components NOT_SERVING.
func newHealthCheck(rpcServer *grpc.Server) *healthCheck {
	srv := health.NewServer()
	for _, service := range []string{healthServicePebble, healthServiceSearch, healthServiceAPI, healthServiceAll} {
		srv.SetServingStatus(service, healthpb.HealthCheckResponse_NOT_SERVING)
	}
	healthpb.RegisterHealthServer(rpcServer, srv)
	return &healthCheck{srv: srv}
}

// markServing marks a component as having completed initialization.
func (h *healthCheck) markServing(service string) {
	h.srv.SetServingStatus(service, healthpb.HealthCheckResponse_SERVING)
}

// markReady marks the ApiServer and the server as a whole ready; call
// after every component init step succeeded, just before Serve.
func (h *healthCheck) markReady() {
	h.markServing(healthServiceAPI)
	h.markServing(healthServiceAll)
}

// shutdown flips every reported status to NOT_SERVING so that health
// probes fail while GracefulStop drains in-flight requests. It must be
// called before GracefulStop; later status changes are ignored.
func (h *healthCheck) shutdown() {
	h.srv.Shutdown()
}
