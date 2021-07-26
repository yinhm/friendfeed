package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	pb "github.com/yinhm/friendfeed/proto"
	"github.com/yinhm/friendfeed/util"
)

func (s *Server) FetchFeed(c *gin.Context, req proto.Message) (profile *pb.Profile, feed *pb.Feed, err error) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	var format = false

	switch req.(type) {
	case *pb.FeedRequest:
		feed, err = s.client.FetchFeed(ctx, req.(*pb.FeedRequest))
		format = true
	case *pb.EntryRequest:
		feed, err = s.client.FetchEntry(ctx, req.(*pb.EntryRequest))
	case *pb.SearchRequest:
		feed, err = s.client.Search(ctx, req.(*pb.SearchRequest))
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

func (s *Server) HomeHandler(c *gin.Context) {
	start := ParseStart(c.Request)
	req := &pb.FeedRequest{
		Id:       "public",
		Start:    int32(start),
		PageSize: 30,
	}

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		glog.Errorf("fetch feed error: %s", err)
		return
	}

	prevStart := req.Start - req.PageSize
	if prevStart < 0 {
		prevStart = 0
	}
	data := pongo2.Context{
		"show_share":  s.feedWritable(c, feed.Uuid),
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"prev_start":  prevStart,
		"next_start":  req.Start + req.PageSize,
		"show_paging": len(feed.Entries) > int(req.PageSize),
	}
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

	prevStart := req.Start - req.PageSize
	if prevStart < 0 {
		prevStart = 0
	}
	data := pongo2.Context{
		"show_header": true,
		"show_share":  contains(feed.Commands, "post"),
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"prev_start":  prevStart,
		"next_start":  req.Start + req.PageSize,
		"show_paging": len(feed.Entries) > int(req.PageSize),
	}
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

	prevStart := req.Start - req.PageSize
	if prevStart < 0 {
		prevStart = 0
	}
	data := pongo2.Context{
		"show_share":  s.feedWritable(c, feed.Uuid),
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"prev_start":  prevStart,
		"next_start":  req.Start + req.PageSize,
		"show_paging": len(feed.Entries) > int(req.PageSize),
	}
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

	prevStart := req.Start - req.PageSize
	if prevStart < 0 {
		prevStart = 0
	}
	data := pongo2.Context{
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"prev_start":  prevStart,
		"next_start":  req.Start + req.PageSize,
		"show_paging": len(feed.Entries) > int(req.PageSize),
		"query":       query,
	}
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

	prevStart := req.Start - req.PageSize
	if prevStart < 0 {
		prevStart = 0
	}
	data := pongo2.Context{
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"prev_start":  prevStart,
		"next_start":  req.Start + req.PageSize,
		"show_paging": len(feed.Entries) > int(req.PageSize),
	}
	s.renderFeed(c, data)
}
