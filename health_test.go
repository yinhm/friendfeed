package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// setupHealthTest starts a real gRPC server with the health service
// registered on a loopback ephemeral port and returns a connected
// health client plus the assembled healthCheck handle.
func setupHealthTest(t *testing.T) (*healthCheck, healthpb.HealthClient) {
	t.Helper()

	rpcServer := grpc.NewServer()
	health := newHealthCheck(rpcServer)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() {
		_ = rpcServer.Serve(lis)
	}()
	t.Cleanup(rpcServer.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	return health, healthpb.NewHealthClient(conn)
}

func checkStatus(t *testing.T, client healthpb.HealthClient, service string) healthpb.HealthCheckResponse_ServingStatus {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.Check(ctx, &healthpb.HealthCheckRequest{Service: service})
	require.NoError(t, err)
	return resp.Status
}

// TestHealthStartupSequence covers the boot path: the health service is
// registered and answers Check, every component starts NOT_SERVING, and
// each init step flips only its own service; overall readiness comes
// last via markReady.
func TestHealthStartupSequence(t *testing.T) {
	health, client := setupHealthTest(t)

	// Freshly registered: nothing is ready yet, not even overall.
	for _, service := range []string{healthServicePebble, healthServiceSearch, healthServiceAPI, healthServiceAll} {
		assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, checkStatus(t, client, service),
			"service %q should start NOT_SERVING", service)
	}

	// Pebble opened: only its own service turns SERVING.
	health.markServing(healthServicePebble)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, checkStatus(t, client, healthServicePebble))
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, checkStatus(t, client, healthServiceSearch))
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, checkStatus(t, client, healthServiceAll))

	// Search index ready: overall still gated on ApiServer readiness.
	health.markServing(healthServiceSearch)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, checkStatus(t, client, healthServiceSearch))
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, checkStatus(t, client, healthServiceAll))

	// All init done: ApiServer and overall report SERVING.
	health.markReady()
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, checkStatus(t, client, healthServiceAPI))
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, checkStatus(t, client, healthServiceAll))
}

// TestHealthComponentNotReady simulates an init failure: a component
// that never reported ready must stay NOT_SERVING, and overall
// readiness must not be inferred from the other components.
func TestHealthComponentNotReady(t *testing.T) {
	health, client := setupHealthTest(t)

	// Pebble init succeeded, search index init failed (or never ran):
	// no status was reported for the remaining components.
	health.markServing(healthServicePebble)

	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, checkStatus(t, client, healthServicePebble))
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, checkStatus(t, client, healthServiceSearch))
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, checkStatus(t, client, healthServiceAPI))
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, checkStatus(t, client, healthServiceAll))
}

// TestHealthShutdown covers the drain path: once shutdown begins every
// status flips to NOT_SERVING so new probes fail, and later attempts to
// mark services ready are ignored.
func TestHealthShutdown(t *testing.T) {
	health, client := setupHealthTest(t)

	health.markServing(healthServicePebble)
	health.markServing(healthServiceSearch)
	health.markReady()
	require.Equal(t, healthpb.HealthCheckResponse_SERVING, checkStatus(t, client, healthServiceAll))

	health.shutdown()
	for _, service := range []string{healthServicePebble, healthServiceSearch, healthServiceAPI, healthServiceAll} {
		assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, checkStatus(t, client, service),
			"service %q should be NOT_SERVING after shutdown", service)
	}

	// Status changes after shutdown are ignored: draining must win.
	health.markReady()
	assert.Equal(t, healthpb.HealthCheckResponse_NOT_SERVING, checkStatus(t, client, healthServiceAll))
}
