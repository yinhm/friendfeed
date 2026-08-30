package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeAPIClient struct {
	pb.ApiClient
	mu       sync.Mutex
	response *pb.AuthenticateFeedApiKeyResponse
	err      error
	calls    int
	token    string
}

func (f *fakeAPIClient) AuthenticateFeedApiKey(_ context.Context, request *pb.AuthenticateFeedApiKeyRequest, _ ...grpc.CallOption) (*pb.AuthenticateFeedApiKeyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.token = request.Token
	return f.response, f.err
}

func (f *fakeAPIClient) observed() (int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.token
}

func validAPIClient() *fakeAPIClient {
	return &fakeAPIClient{response: &pb.AuthenticateFeedApiKeyResponse{
		FeedUuid: "11111111-1111-1111-1111-111111111111", KeyId: []byte("12345678"),
	}}
}

func apiTestRouter(h *Handler, endpoint gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1", h.transportBoundary())
	group.GET("/test", endpoint)
	return router
}

func apiRequest(t *testing.T, router http.Handler, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestTransportRejectsEveryMalformedCredentialWithSameShape(t *testing.T) {
	client := validAPIClient()
	h := New(client)
	router := apiTestRouter(h, func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	for _, value := range []string{"", "Basic abc", "bearer abc", "Bearer", "Bearer ", "Bearer a b"} {
		recorder := apiRequest(t, router, value)
		require.Equal(t, http.StatusUnauthorized, recorder.Code, value)
		require.Contains(t, recorder.Body.String(), `"code":"invalid_api_key"`)
		require.NotEmpty(t, jsonRequestID(t, recorder.Body.String()))
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
		require.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"))
	}
	calls, _ := client.observed()
	require.Zero(t, calls)
}

func jsonRequestID(t *testing.T, body string) string {
	t.Helper()
	const marker = `"request_id":"`
	start := strings.Index(body, marker)
	require.NotEqual(t, -1, start)
	start += len(marker)
	end := strings.Index(body[start:], `"`)
	require.NotEqual(t, -1, end)
	return body[start : start+end]
}

func TestTransportDoesNotExposeAuthorization(t *testing.T) {
	client := validAPIClient()
	h := New(client)
	router := apiTestRouter(h, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"authenticated": principal(c).FeedUUID != ""})
	})
	recorder := apiRequest(t, router, "Bearer super-secret")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "super-secret")
	calls, token := client.observed()
	require.Equal(t, 1, calls)
	require.Equal(t, "super-secret", token)
}

func TestTransportMapsUnknownAndRevokedCredentialsToUniformUnauthorized(t *testing.T) {
	for _, authErr := range []error{
		status.Error(codes.Unauthenticated, "unknown key"),
		status.Error(codes.Unauthenticated, "revoked key"),
	} {
		client := &fakeAPIClient{err: authErr}
		recorder := apiRequest(t, apiTestRouter(New(client), func(c *gin.Context) {
			t.Fatal("unauthenticated request reached endpoint")
		}), "Bearer syntactically-valid")
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		require.Contains(t, recorder.Body.String(), `"code":"invalid_api_key"`)
		require.NotContains(t, recorder.Body.String(), status.Convert(authErr).Message())
	}
}

func TestTransportRejectsWhenConcurrencySlotsAreFull(t *testing.T) {
	h := New(validAPIClient())
	h.sem = make(chan struct{}, 1)
	entered := make(chan struct{})
	release := make(chan struct{})
	router := apiTestRouter(h, func(c *gin.Context) {
		close(entered)
		<-release
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- apiRequest(t, router, "Bearer first") }()
	<-entered
	second := apiRequest(t, router, "Bearer second")
	require.Equal(t, http.StatusServiceUnavailable, second.Code)
	require.Contains(t, second.Body.String(), `"code":"unavailable"`)
	close(release)
	require.Equal(t, http.StatusOK, (<-firstDone).Code)
}

func TestShutdownCancelsInflightRequest(t *testing.T) {
	h := New(validAPIClient())
	router := apiTestRouter(h, func(c *gin.Context) {
		<-c.Request.Context().Done()
		writeError(c, http.StatusServiceUnavailable, "unavailable", "Service unavailable")
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- apiRequest(t, router, "Bearer key") }()
	time.Sleep(10 * time.Millisecond)
	h.Shutdown()
	select {
	case recorder := <-done:
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	case <-time.After(time.Second):
		t.Fatal("request was not canceled by shutdown")
	}
}

func TestTransportBoundsRequestBody(t *testing.T) {
	h := New(validAPIClient())
	router := apiTestRouter(h, func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		require.Error(t, err)
		writeError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "Payload too large")
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", strings.NewReader(strings.Repeat("x", maxRequestBytes+1)))
	req.Header.Set("Authorization", "Bearer key")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestTransportTimeoutIsApplied(t *testing.T) {
	h := New(validAPIClient())
	h.timeout = time.Millisecond
	router := apiTestRouter(h, func(c *gin.Context) {
		<-c.Request.Context().Done()
		require.ErrorIs(t, c.Request.Context().Err(), context.DeadlineExceeded)
		writeError(c, http.StatusServiceUnavailable, "unavailable", "Service unavailable")
	})
	recorder := apiRequest(t, router, "Bearer key")
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}
