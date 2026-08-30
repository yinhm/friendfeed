package server

import (
	"encoding/base64"
	"net/http"
	"net/url"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type feedApiKeyStatusView struct {
	KeyID       string `json:"key_id,omitempty"`
	CreatedAtMs int64  `json:"created_at_ms,omitempty"`
	RotatedAtMs int64  `json:"rotated_at_ms,omitempty"`
	RevokedAtMs int64  `json:"revoked_at_ms,omitempty"`
	Active      bool   `json:"active"`
}

type feedApiKeyPageData struct {
	Feed   groupFormView        `json:"feed"`
	Status feedApiKeyStatusView `json:"status"`
}

func feedApiKeyStatusViewFromProto(in *pb.FeedApiKeyStatusResponse) feedApiKeyStatusView {
	if in == nil {
		return feedApiKeyStatusView{}
	}
	return feedApiKeyStatusView{
		KeyID:       base64.RawURLEncoding.EncodeToString(in.KeyId),
		CreatedAtMs: in.CreatedAtMs,
		RotatedAtMs: in.RotatedAtMs,
		RevokedAtMs: in.RevokedAtMs,
		Active:      in.Active,
	}
}

func (s *Server) feedApiKeyPageFeed(c *gin.Context) (*pb.Feed, bool) {
	name := c.Params.ByName("name")
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	feed, err := s.client.FetchFeed(ctx, &pb.FeedRequest{Id: name, PageSize: 1, ViewerUuid: CurrentUserUuid(c)})
	if RequestError(c, err) {
		return nil, false
	}
	if location, renamed := renamedFeedLocationWithSuffix(name, feed, "api", c.Request.URL.RawQuery); renamed {
		c.Redirect(http.StatusFound, location)
		return nil, false
	}
	return feed, true
}

func (s *Server) FeedApiKeyPageHandler(c *gin.Context) {
	feed, ok := s.feedApiKeyPageFeed(c)
	if !ok {
		return
	}
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	statusResponse, err := s.client.GetFeedApiKeyStatus(ctx, &pb.FeedApiKeyManageRequest{
		ActorUuid: CurrentUserUuid(c), FeedUuid: feed.Uuid,
	})
	if RequestError(c, err) {
		return
	}
	encoded, err := marshalPageBootstrap("feed-api-key", feedApiKeyPageData{
		Feed:   groupFormView{ID: feed.Id, Name: feed.Name, Description: feed.Description, Picture: feed.Picture, Private: feed.Private, Type: feed.Type},
		Status: feedApiKeyStatusViewFromProto(statusResponse),
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "Server error.")
		return
	}
	s.HTML(c, http.StatusOK, "app_shell.html", pongo2.Context{"title": "Feed API", "pageBootstrap": string(encoded)})
}

func feedApiKeyActionHTTPError(c *gin.Context, err error) {
	code := http.StatusInternalServerError
	switch status.Code(err) {
	case codes.InvalidArgument:
		code = http.StatusBadRequest
	case codes.PermissionDenied:
		code = http.StatusForbidden
	case codes.NotFound:
		code = http.StatusNotFound
	case codes.AlreadyExists, codes.FailedPrecondition:
		code = http.StatusConflict
	case codes.Unavailable, codes.DeadlineExceeded:
		code = http.StatusServiceUnavailable
	}
	c.JSON(code, gin.H{"error": status.Convert(err).Message()})
}

func (s *Server) FeedApiKeyActionHandler(c *gin.Context) {
	feed, ok := s.feedApiKeyPageFeed(c)
	if !ok {
		return
	}
	request := &pb.FeedApiKeyManageRequest{ActorUuid: CurrentUserUuid(c), FeedUuid: feed.Uuid}
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	var response any
	var err error
	switch c.Params.ByName("action") {
	case "generate":
		var result *pb.FeedApiKeyMutationResponse
		result, err = s.client.GenerateFeedApiKey(ctx, request)
		if result != nil {
			response = gin.H{"status": feedApiKeyStatusViewFromProto(result.Status), "token": result.Token}
		}
	case "rotate":
		var result *pb.FeedApiKeyMutationResponse
		result, err = s.client.RotateFeedApiKey(ctx, request)
		if result != nil {
			response = gin.H{"status": feedApiKeyStatusViewFromProto(result.Status), "token": result.Token}
		}
	case "revoke":
		var result *pb.FeedApiKeyStatusResponse
		result, err = s.client.RevokeFeedApiKey(ctx, request)
		if result != nil {
			response = gin.H{"status": feedApiKeyStatusViewFromProto(result)}
		}
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "Unknown Feed API key action"})
		return
	}
	if err != nil {
		feedApiKeyActionHTTPError(c, err)
		return
	}
	// Write through Gin only after the RPC succeeds; plaintext token is never
	// put in page bootstrap, redirect locations, cookies or server-side state.
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}

func FeedApiKeyURL(feedID string) string {
	return "/feed/" + url.PathEscape(feedID) + "/api"
}
