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
	var basetime time.Time
	for _, e := range feed.Entries {
		e.RebuildCommand(profile, graph)
		basetime, _ = time.Parse(time.RFC3339, e.Date)
		e.Date = util.FormatTime(basetime)

		if format {
			e.FormatComments(int32(0))
			e.FormatLikes(int32(0))
			ellipsis := fmt.Sprintf("<a href=\"/e/%s\" style=\"padding-left: 30px;\">Read more...</a>", e.Id)
			e.Body = util.Truncate(e.Body, 300, ellipsis)
		}
		e.RebuildCommentsCommand(profile, graph)
	}
	return
}

func feedContext(feed *pb.Feed, start, pageSize int32) pongo2.Context {
	prevStart := max(start-pageSize, 0)
	return pongo2.Context{
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"prev_start":  prevStart,
		"next_start":  start + pageSize,
		"show_paging": len(feed.Entries) > int(pageSize),
		"show_share":  false,
	}
}

func renamedFeedLocation(requestedID string, feed *pb.Feed, rawQuery string) (string, bool) {
	if feed == nil || feed.Id == "" || feed.Id == requestedID {
		return "", false
	}
	location := "/feed/" + url.PathEscape(feed.Id)
	if rawQuery != "" {
		location += "?" + rawQuery
	}
	return location, true
}

func (s *Server) HomeHandler(c *gin.Context) {
	userUuid := CurrentUserUuid(c)
	if userUuid == "" {
		http.Redirect(c.Writer, c.Request, "/public", http.StatusFound)
		return
	}

	start := ParseStart(c.Request)
	req := &pb.FeedRequest{
		Id:          "home",
		ProfileUuid: userUuid,
		Start:       int32(start),
		PageSize:    30,
	}

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		log.Printf("fetch feed error: %v", err)
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	data["show_share"] = s.feedWritable(c, feed.Uuid)
	s.renderFeed(c, data)
}

func (s *Server) FeedHandler(c *gin.Context) {
	feedname := c.Params.ByName("name")
	start := ParseStart(c.Request)
	req := &pb.FeedRequest{
		Id:       feedname,
		Start:    int32(start),
		PageSize: 30,
	}
	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}
	if feed.Private && !s.feedReadable(c, feed.Uuid) {
		c.HTML(http.StatusForbidden, "403.html", pongo2.Context{})
		return
	}
	if location, renamed := renamedFeedLocation(feedname, feed, c.Request.URL.RawQuery); renamed {
		// Rename metadata is periodically reclaimed, so do not let clients
		// cache this redirect permanently after the old ID becomes reusable.
		c.Redirect(http.StatusFound, location)
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	data["show_header"] = true
	s.renderFeed(c, data)
}

func (s *Server) PublicHandler(c *gin.Context) {
	start := ParseStart(c.Request)
	req := &pb.FeedRequest{
		Id:       "public",
		Start:    int32(start),
		PageSize: 30,
	}

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	// s.HTML(c, 200, "_feed.html", data)
	s.renderFeed(c, data)
}

func (s *Server) SearchHandler(c *gin.Context) {
	reqQuery := c.Request.URL.Query()
	query := reqQuery.Get("q")
	start := ParseStart(c.Request)
	req := &pb.SearchRequest{
		Query:    query,
		Start:    int32(start),
		PageSize: 30,
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
		Query:    "#" + name,
		Start:    int32(start),
		PageSize: 30,
	}

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	s.renderFeed(c, data)
}
