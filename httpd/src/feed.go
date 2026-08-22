package server

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (s *Server) FetchFeed(c *gin.Context, req proto.Message) (profile *pb.Profile, feed *pb.Feed, err error) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	var format = false

	switch r := req.(type) {
	case *pb.FeedRequest:
		feed, err = s.client.FetchFeed(ctx, r)
		format = true
	case *pb.EntryRequest:
		feed, err = s.client.FetchEntry(ctx, r)
	case *pb.SearchRequest:
		feed, err = s.client.Search(ctx, r)
		format = true
	}
	if err != nil {
		return
	}
	profile, err = s.CurrentUser(c)
	if err != nil {
		return
	}

	// IMPORTANT:
	// rewrite public uuid to current user uuid
	if feed.Uuid == "Public" {
		feed.Uuid = profile.Uuid
	}

	// check user.IsFollowed(feed)
	if _, ok := req.(*pb.FeedRequest); ok {
		s.BuildFollow(profile, feed)
	}

	graph, err := s.CurrentGraph(c)
	if err != nil {
		return
	}
	for _, e := range feed.Entries {
		prepareFeedEntry(e, profile, graph, format)
	}
	return
}

// prepareFeedEntry is the shared httpd presentation boundary for every feed
// page. RPCs choose which entries belong to a feed; collapsing interactions,
// formatting time/body and rebuilding commands must not vary by feed kind.
func prepareFeedEntry(entry *pb.Entry, profile *pb.Profile, graph *pb.Graph, format bool) {
	entry.RebuildCommand(profile, graph)
	if basetime, err := time.Parse(time.RFC3339, entry.Date); err == nil {
		entry.Date = util.FormatTime(basetime)
	}
	if format {
		entry.FormatComments(0)
		entry.FormatLikes(0)
		ellipsis := fmt.Sprintf("<a href=\"/e/%s\" style=\"padding-left: 30px;\">Read more...</a>", entry.Id)
		entry.Body = util.Truncate(entry.Body, 300, ellipsis)
	}
	entry.RebuildCommentsCommand(profile, graph)
}

func feedContext(feed *pb.Feed, start, pageSize int32) pongo2.Context {
	prevStart := max(start-pageSize, 0)
	// The lookahead entry (one more than pageSize) signals a following page;
	// the previous page depends only on the current offset. Splitting the two
	// keeps Prev reachable on a short last page.
	showNext := len(feed.Entries) > int(pageSize)
	if showNext {
		feed.Entries = feed.Entries[:pageSize]
	}
	showPrev := start > 0
	return pongo2.Context{
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"prev_start":  prevStart,
		"next_start":  start + pageSize,
		"show_prev":   showPrev,
		"show_next":   showNext,
		"show_paging": showPrev || showNext,
		"show_share":  false,
	}
}

func cursorFeedContext(feed *pb.Feed) pongo2.Context {
	return pongo2.Context{
		"title":         feed.Id,
		"name":          feed.Id,
		"feed":          feed,
		"prev_start":    int32(0),
		"next_start":    int32(0),
		"next_cursor":   feed.NextCursor,
		"cursor_paging": true,
		"show_paging":   feed.NextCursor != "",
		"show_share":    false,
	}
}

// configureFeedPagination preserves explicit legacy ?start=N links. Cursor
// pagination is the default for new profile/timeline requests, and wins when
// both styles are present.
func configureFeedPagination(r *http.Request, req *pb.FeedRequest) bool {
	query := r.URL.Query()
	cursor := query.Get("cursor")
	if cursor != "" || !query.Has("start") {
		req.Cursor = cursor
		req.CursorPaging = true
		return true
	}
	req.Start = int32(ParseStart(r))
	return false
}

func renamedFeedLocation(requestedID string, feed *pb.Feed, rawQuery string) (string, bool) {
	return renamedFeedLocationWithSuffix(requestedID, feed, "", rawQuery)
}

func renamedFeedLocationWithSuffix(requestedID string, feed *pb.Feed, suffix, rawQuery string) (string, bool) {
	if feed == nil || feed.Id == "" || feed.Id == requestedID {
		return "", false
	}
	location := "/feed/" + url.PathEscape(feed.Id)
	if suffix == "likes" || suffix == "comments" {
		location += "/" + suffix
	}
	if rawQuery != "" {
		location += "?" + rawQuery
	}
	return location, true
}

func (s *Server) InteractionFeedHandler(kind pb.InteractionKind, suffix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		baseReq := &pb.FeedRequest{
			Id:           name,
			PageSize:     1,
			CursorPaging: true,
			ViewerUuid:   CurrentUserUuid(c),
		}
		_, base, err := s.FetchFeed(c, baseReq)
		if RequestError(c, err) {
			return
		}
		if location, renamed := renamedFeedLocationWithSuffix(name, base, suffix, c.Request.URL.RawQuery); renamed {
			c.Redirect(http.StatusFound, location)
			return
		}
		ctx, cancel := DefaultTimeoutContext()
		defer cancel()
		response, err := s.client.FetchInteractionFeed(ctx, &pb.InteractionFeedRequest{
			ProfileUuid: base.Uuid, ViewerUuid: CurrentUserUuid(c), Kind: kind,
			Cursor: c.Query("cursor"), PageSize: 30,
		})
		if RequestError(c, err) {
			return
		}
		profile, err := s.CurrentUser(c)
		if RequestError(c, err) {
			return
		}
		graph, err := s.CurrentGraph(c)
		if RequestError(c, err) {
			return
		}
		feed := interactionFeedForDisplay(response, profile, graph)
		data := cursorFeedContext(feed)
		data["show_header"] = true
		data["title"] = feed.Id + " " + suffix
		s.renderFeed(c, data)
	}
}

func interactionFeedForDisplay(response *pb.InteractionFeedResponse, profile *pb.Profile, graph *pb.Graph) *pb.Feed {
	feed := &pb.Feed{
		Uuid: response.Profile.Uuid, Id: response.Profile.Id, Name: response.Profile.Name,
		Picture: response.Profile.Picture, Description: response.Profile.Description,
		Type: response.Profile.Type, Private: response.Profile.Private, NextCursor: response.NextCursor,
	}
	for _, item := range response.Items {
		if item == nil || item.Entry == nil {
			continue
		}
		// Keep the complete hydrated interaction lists on the Entry. The
		// item-level Like/latest Comment identifies why the Entry is indexed;
		// it is not a display filter.
		prepareFeedEntry(item.Entry, profile, graph, true)
		feed.Entries = append(feed.Entries, item.Entry)
	}
	return feed
}

func (s *Server) HomeHandler(c *gin.Context) {
	userUuid := CurrentUserUuid(c)
	if userUuid == "" {
		http.Redirect(c.Writer, c.Request, "/public", http.StatusFound)
		return
	}

	req := &pb.FeedRequest{
		Id:          "home",
		ProfileUuid: userUuid,
		PageSize:    30,
		ViewerUuid:  userUuid,
	}
	cursorPaging := configureFeedPagination(c.Request, req)

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		log.Printf("fetch feed error: %v", err)
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	if cursorPaging {
		data = cursorFeedContext(feed)
	}
	data["show_share"] = s.feedWritable(c, feed.Uuid)
	// Realtime belongs only to the newest Home page. The existing cursor is
	// an older-page position, never a "since" token.
	data["realtime_home"] = req.Cursor == "" && req.Start == 0
	s.renderFeed(c, data)
}

func (s *Server) FeedHandler(c *gin.Context) {
	feedname := c.Params.ByName("name")
	req := &pb.FeedRequest{
		Id:         feedname,
		PageSize:   30,
		ViewerUuid: CurrentUserUuid(c),
	}
	cursorPaging := configureFeedPagination(c.Request, req)
	_, feed, err := s.FetchFeed(c, req)
	if err != nil && status.Code(err) == codes.PermissionDenied {
		// Private feed the viewer may not read: offer the follow-request
		// entry point instead of a bare 403.
		s.renderPrivateFeedRequest(c, feedname, "")
		return
	}
	if RequestError(c, err) {
		return
	}
	if location, renamed := renamedFeedLocation(feedname, feed, c.Request.URL.RawQuery); renamed {
		// Rename metadata is periodically reclaimed, so do not let clients
		// cache this redirect permanently after the old ID becomes reusable.
		c.Redirect(http.StatusFound, location)
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	if cursorPaging {
		data = cursorFeedContext(feed)
	}
	data["show_header"] = true
	if actor := CurrentUserUuid(c); actor != "" {
		ctx, cancel := DefaultTimeoutContext()
		_, manageErr := s.client.ListFeedServices(ctx, &pb.ListFeedServicesRequest{
			ActorUuid: actor, TargetFeedUuid: feed.Uuid,
		})
		cancel()
		if manageErr == nil {
			data["manage_services_url"] = "/account/feed/" + url.PathEscape(feed.Uuid) + "/import"
		}

		// Group feeds offer admin/super viewers entry points to the
		// settings and members pages (docs/group.md manage_group rule).
		if feed.Type == "group" {
			ctx, cancel := DefaultTimeoutContext()
			view, viewErr := s.client.GetGroup(ctx, &pb.GetGroupRequest{
				GroupUuid: feed.Uuid, ViewerUuid: actor,
			})
			cancel()
			if viewErr == nil {
				profile, _ := s.CurrentUser(c)
				if canManageGroup(view, profile) {
					data["group_settings_url"] = "/groups/" + url.PathEscape(feed.Id) + "/settings"
					data["group_members_url"] = "/groups/" + url.PathEscape(feed.Id) + "/members"
				}
			}
		}
	}
	s.renderFeed(c, data)
}

func (s *Server) PublicHandler(c *gin.Context) {
	req := &pb.FeedRequest{
		Id:         "public",
		PageSize:   30,
		ViewerUuid: CurrentUserUuid(c),
	}
	cursorPaging := configureFeedPagination(c.Request, req)

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	// Public now reads from the shared TimelineIndex, which trims to exactly
	// PageSize entries and reports older pages via NextCursor. Legacy
	// ?start=N links still render, but only cursor mode shows a Next link.
	data := feedContext(feed, req.Start, req.PageSize)
	if cursorPaging {
		data = cursorFeedContext(feed)
	}
	s.renderFeed(c, data)
}

func (s *Server) SearchHandler(c *gin.Context) {
	reqQuery := c.Request.URL.Query()
	query := reqQuery.Get("q")
	start := ParseStart(c.Request)
	req := &pb.SearchRequest{
		Query:      query,
		Start:      int32(start),
		PageSize:   30,
		ViewerUuid: CurrentUserUuid(c),
	}

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	data["query"] = query
	s.renderFeed(c, data)
}

func (s *Server) TagHandler(c *gin.Context) {
	name := c.Params.ByName("name")
	start := ParseStart(c.Request)
	req := &pb.SearchRequest{
		Query:      "#" + name,
		Start:      int32(start),
		PageSize:   30,
		ViewerUuid: CurrentUserUuid(c),
	}

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	s.renderFeed(c, data)
}
