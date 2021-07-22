package server

import (
	"net/http"
	"strings"
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

	showShare := s.feedWritable(c, "home")
	prevStart := req.Start - req.PageSize
	if prevStart < 0 {
		prevStart = 0
	}
	data := pongo2.Context{
		"show_share":  showShare,
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"prev_start":  prevStart,
		"next_start":  req.Start + req.PageSize,
		"show_paging": true,
	}
	s.renderFeed(c, data)
}

func (s *Server) FeedHandler(c *gin.Context) {
	feedname := c.Params.ByName("name")
	if feedname == "" {
		feedname = "Home"
	}
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
	if feed.Private && !s.feedReadable(c, feed.Id) {
		c.HTML(http.StatusForbidden, "403.html", pongo2.Context{})
		return
	}

	showHeader := feed.Id != "Home" && !strings.HasPrefix(feed.Id, "e/")
	showShare := feed.Id == "Home" || contains(feed.Commands, "post")
	showDirect := contains(feed.Commands, "dm")
	prevStart := req.Start - req.PageSize
	if prevStart < 0 {
		prevStart = 0
	}
	data := pongo2.Context{
		"show_header": showHeader,
		"show_share":  showShare,
		"show_direct": showDirect,
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"ff_username": "me",
		"prev_start":  prevStart,
		"next_start":  req.Start + req.PageSize,
		"show_paging": true,
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

	showShare := s.feedWritable(c, "public")
	prevStart := req.Start - req.PageSize
	if prevStart < 0 {
		prevStart = 0
	}
	data := pongo2.Context{
		"show_share":  showShare,
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"prev_start":  prevStart,
		"next_start":  req.Start + req.PageSize,
		"show_paging": true,
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
		"show_paging": true,
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
		"show_paging": true,
	}
	s.renderFeed(c, data)
}
