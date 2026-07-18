package server

import (
	"log"
	"net/http"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
)

func (s *Server) AccountHandler(c *gin.Context) {
	c.Redirect(http.StatusFound, "/account/import")
}

func (s *Server) ImportHandler(c *gin.Context) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	uuid := CurrentUserUuid(c)
	if uuid == "" {
		c.String(http.StatusBadRequest, "please login first")
		return
	}
	req := &pb.ProfileRequest{Uuid: uuid}
	graph, err := s.client.FetchGraph(ctx, req)
	if err != nil {
		RequestError(c, err)
		return
	}

	data := pongo2.Context{
		"title": "Import services",
		"graph": graph,
	}
	s.HTML(c, 200, "import.html", data)
}

func (s *Server) TwitterImportHandler(c *gin.Context) {
	c.Redirect(http.StatusFound, "/auth/twitter")
}

func (s *Server) DeleteServiceHandler(c *gin.Context) {
	service := c.Params.ByName("service")
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	uuid := CurrentUserUuid(c)
	if uuid == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	req := &pb.ServiceRequest{
		User:    uuid,
		Service: service,
	}
	_, err := s.client.DeleteService(ctx, req)
	if err != nil {
		log.Printf("Error on deleting: %s, %s", uuid, err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	c.Redirect(http.StatusFound, "/account/import")
}

func (s *Server) BuildFollow(profile *pb.Profile, feed *pb.Feed) {
	if feed.Uuid == profile.Uuid {
		feed.Commands = []string{"post"}
		return
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	fReq := &pb.FollowRequest{
		ProfileUuid: profile.Uuid,
		FeedUuid:    feed.Uuid,
		Action:      "isFollow",
	}
	fResp, err := s.client.GraphFollow(ctx, fReq)
	if err != nil {
		return
	}
	if fResp.Followed {
		feed.Commands = []string{"unfollow"}
		if feed.Type == "group" {
			feed.Commands = append(feed.Commands, "post")
		}
	} else {
		feed.Commands = []string{"follow"}
	}
}
