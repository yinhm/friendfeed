package server

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/status"
)

// renderPrivateFeedRequest renders the SSR entry point for a private feed
// the viewer may not read: the feed's metadata plus the follow-request
// action matching the viewer's state (anonymous / none / pending). Content
// stays closed; this page only carries the approval workflow.
func (s *Server) renderPrivateFeedRequest(c *gin.Context, feedID, errMsg string) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	profile, err := s.client.FetchProfile(ctx, &pb.ProfileRequest{Id: feedID})
	if RequestError(c, err) {
		return
	}
	profile.Picture = PictureOrDefault(profile.Picture)

	state := "none"
	viewer := CurrentUserUuid(c)
	if viewer == "" {
		state = "anonymous"
	} else {
		resp, err := s.client.GraphFollow(ctx, &pb.FollowRequest{
			ProfileUuid: viewer,
			FeedUuid:    profile.Uuid,
			Action:      "",
		})
		if RequestError(c, err) {
			return
		}
		switch {
		case resp.Followed:
			state = "following"
		case resp.Requested:
			state = "pending"
		}
	}

	s.HTML(c, http.StatusOK, "feed_private.html", pongo2.Context{
		"title":   profile.Name,
		"profile": profile,
		"state":   state,
		"error":   errMsg,
	})
}

// FeedRequestHandler files the session user's follow request against a
// private feed, then returns to the feed page (which now shows the pending
// state). Server-side validation rejects non-private targets.
func (s *Server) FeedRequestHandler(c *gin.Context) {
	c.Request.ParseForm()
	feedUUID := c.Request.Form.Get("feed_uuid")
	feedID := c.Request.Form.Get("feed_id")
	if feedUUID == "" || feedID == "" {
		c.String(http.StatusBadRequest, "feed_uuid and feed_id are required")
		return
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	_, err := s.client.RequestFollow(ctx, &pb.RequestFollowRequest{
		ActorUuid: CurrentUserUuid(c),
		FeedUuid:  feedUUID,
	})
	if err != nil {
		s.renderPrivateFeedRequest(c, feedID, status.Convert(err).Message())
		return
	}
	c.Redirect(http.StatusSeeOther, "/feed/"+url.PathEscape(feedID))
}

// FeedRequestCancelHandler withdraws the session user's own pending request.
func (s *Server) FeedRequestCancelHandler(c *gin.Context) {
	c.Request.ParseForm()
	feedUUID := c.Request.Form.Get("feed_uuid")
	feedID := c.Request.Form.Get("feed_id")
	if feedUUID == "" || feedID == "" {
		c.String(http.StatusBadRequest, "feed_uuid and feed_id are required")
		return
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	_, err := s.client.CancelFollowRequest(ctx, &pb.RequestFollowRequest{
		ActorUuid: CurrentUserUuid(c),
		FeedUuid:  feedUUID,
	})
	if err != nil {
		s.renderPrivateFeedRequest(c, feedID, status.Convert(err).Message())
		return
	}
	c.Redirect(http.StatusSeeOther, "/feed/"+url.PathEscape(feedID))
}

// renderAccountRequests lists the pending follow requests against the
// session user's own feed; approving is the private-feed counterpart of the
// Group members page.
func (s *Server) renderAccountRequests(c *gin.Context, errMsg string) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	currentUser, err := s.CurrentUser(c)
	if RequestError(c, err) {
		return
	}
	resp, err := s.client.ListFollowRequests(ctx, &pb.ListFollowRequestsRequest{
		ActorUuid: currentUser.Uuid,
		FeedUuid:  currentUser.Uuid,
		Limit:     200,
	})
	if RequestError(c, err) {
		return
	}
	for _, r := range resp.Requests {
		if r != nil && r.Requester != nil {
			r.Requester.Picture = PictureOrDefault(r.Requester.Picture)
		}
	}
	encoded, err := marshalPageBootstrap("requests", requestsPageDataFromProto(resp.Requests, currentUser.Private, errMsg))
	if err != nil {
		c.String(http.StatusInternalServerError, "Server error.")
		return
	}
	s.HTML(c, http.StatusOK, "app_shell.html", pongo2.Context{"title": "Follow requests", "pageBootstrap": string(encoded)})
}

func (s *Server) AccountRequestsHandler(c *gin.Context) {
	s.renderAccountRequests(c, "")
}

// AccountRequestActionHandler approves or rejects one request against the
// session user's own feed. Authorization (actor owns the feed) is enforced
// server-side.
func (s *Server) AccountRequestActionHandler(c *gin.Context) {
	c.Request.ParseForm()
	action := c.Request.Form.Get("action")
	target := strings.TrimSpace(c.Request.Form.Get("target_uuid"))
	if target == "" {
		c.String(http.StatusBadRequest, "target_uuid is required")
		return
	}

	req := &pb.FollowRequestAction{
		ActorUuid:  CurrentUserUuid(c),
		FeedUuid:   CurrentUserUuid(c),
		TargetUuid: target,
	}
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	var err error
	switch action {
	case "approve":
		_, err = s.client.ApproveFollowRequest(ctx, req)
	case "reject":
		_, err = s.client.RejectFollowRequest(ctx, req)
	default:
		c.String(http.StatusBadRequest, "unknown action")
		return
	}
	if err != nil {
		s.renderAccountRequests(c, status.Convert(err).Message())
		return
	}
	c.Redirect(http.StatusFound, "/account/requests")
}
