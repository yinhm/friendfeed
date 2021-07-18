package server

import (
	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	pb "github.com/yinhm/friendfeed/proto"
)

func (s *Server) SearchHandler(c *gin.Context) {
	start := ParseStart(c.Request)
	req := &pb.SearchRequest{
		Query:    "孤独",
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
	// s.HTML(c, 200, "_feed.html", data)
	s.renderFeed(c, data)
}
