// Package api implements the versioned, machine-facing HTTP API boundary.
// It is deliberately separate from the cookie-authenticated Browser BFF.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultConcurrency = 32
	defaultTimeout     = 30 * time.Second
	maxRequestBytes    = media.MaxUploadFileBytes + 64<<10
	requestIDKey       = "public-api-request-id"
	bearerTokenKey     = "public-api-bearer-token"
	principalKey       = "public-api-principal"
)

type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type errorEnvelope struct {
	Error Error `json:"error"`
}

type Principal struct {
	FeedUUID string
	KeyID    []byte
}

// Handler owns the bounded resources and shutdown signal for Public API V1.
type Handler struct {
	client  pb.ApiClient
	ctx     context.Context
	cancel  context.CancelFunc
	sem     chan struct{}
	timeout time.Duration
	once    sync.Once
}

func New(client pb.ApiClient) *Handler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Handler{client: client, ctx: ctx, cancel: cancel,
		sem: make(chan struct{}, defaultConcurrency), timeout: defaultTimeout}
}

// Register installs only the versioned machine API. Browser BFF routes retain
// their existing session and rendering behavior outside this group.
func (h *Handler) Register(router *gin.Engine) {
	group := router.Group("/api/v1")
	group.Use(h.transportBoundary())
	// Data endpoints are registered by later phases. Unknown API paths still
	// obey the stable JSON error contract; this is not a debug endpoint.
	group.Any("/*path", func(c *gin.Context) {
		writeError(c, http.StatusNotFound, "not_found", "Resource not found")
	})
}

func (h *Handler) Shutdown() { h.once.Do(h.cancel) }

func (h *Handler) transportBoundary() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Type", "application/json; charset=utf-8")
		c.Set(requestIDKey, newRequestID())

		select {
		case h.sem <- struct{}{}:
			defer func() { <-h.sem }()
		default:
			writeError(c, http.StatusServiceUnavailable, "unavailable", "Service unavailable")
			c.Abort()
			return
		}

		token, ok := strictBearerToken(c.Request.Header.Values("Authorization"))
		if !ok {
			writeError(c, http.StatusUnauthorized, "invalid_api_key", "API key is invalid")
			c.Abort()
			return
		}
		c.Set(bearerTokenKey, token)
		ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
		defer cancel()
		stopShutdownCancellation := context.AfterFunc(h.ctx, cancel)
		defer stopShutdownCancellation()
		c.Request = c.Request.WithContext(ctx)
		principal, err := h.client.AuthenticateFeedApiKey(ctx, &pb.AuthenticateFeedApiKeyRequest{Token: token})
		if err != nil || principal == nil || principal.FeedUuid == "" || len(principal.KeyId) == 0 {
			switch status.Code(err) {
			case codes.FailedPrecondition, codes.PermissionDenied:
				writeError(c, http.StatusForbidden, "forbidden", "Feed is unavailable")
			case codes.Unavailable, codes.DeadlineExceeded:
				writeError(c, http.StatusServiceUnavailable, "unavailable", "Service unavailable")
			default:
				writeError(c, http.StatusUnauthorized, "invalid_api_key", "API key is invalid")
			}
			c.Abort()
			return
		}
		c.Set(principalKey, Principal{FeedUUID: principal.FeedUuid, KeyID: append([]byte(nil), principal.KeyId...)})
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
		c.Next()
	}
}

func principal(c *gin.Context) Principal {
	value, _ := c.Get(principalKey)
	result, _ := value.(Principal)
	return result
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	// rand.Read failing means the host cannot supply secure randomness. The ID
	// is diagnostic rather than an authenticator; a timestamp remains useful
	// for correlating the safe error response with the server log.
	return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
}

func strictBearerToken(values []string) (string, bool) {
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	if token == "" || strings.TrimSpace(token) != token || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}

func bearerToken(c *gin.Context) string {
	value, _ := c.Get(bearerTokenKey)
	token, _ := value.(string)
	return token
}

func requestID(c *gin.Context) string {
	value, _ := c.Get(requestIDKey)
	id, _ := value.(string)
	return id
}

func writeError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorEnvelope{Error: Error{
		Code: code, Message: message, RequestID: requestID(c),
	}})
}
