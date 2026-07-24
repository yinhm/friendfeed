package server

import (
	"log"
	"net/http"
	"strings"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
)

func (s *Server) AccountHandler(c *gin.Context) {
	c.Redirect(http.StatusFound, "/account/profile")
}

func (s *Server) AccountProfileHandler(c *gin.Context) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	uuid := CurrentUserUuid(c)
	if uuid == "" {
		c.String(http.StatusUnauthorized, "please login first")
		return
	}

	req := &pb.ProfileRequest{Uuid: uuid}
	profile, err := s.client.FetchProfile(ctx, req)
	if err != nil {
		RequestError(c, err)
		return
	}

	data := pongo2.Context{
		"title":   "Edit Profile",
		"profile": profile,
	}
	s.HTML(c, 200, "account_profile.html", data)
}

func (s *Server) AccountProfileUpdateHandler(c *gin.Context) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	uuid := CurrentUserUuid(c)
	if uuid == "" {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// Fetch current profile to preserve system fields
	req := &pb.ProfileRequest{Uuid: uuid}
	currentProfile, err := s.client.FetchProfile(ctx, req)
	if err != nil {
		log.Printf("FetchProfile error: %s", err)
		c.String(http.StatusInternalServerError, "Failed to fetch current profile")
		return
	}

	// Build Feedinfo from form data (Feedinfo and Profile are the same thing)
	feedinfo := &pb.Feedinfo{
		Uuid:        uuid,
		Id:          strings.TrimSpace(c.PostForm("id")),
		Name:        strings.TrimSpace(c.PostForm("name")),
		Description: strings.TrimSpace(c.PostForm("description")),
		Picture:     strings.TrimSpace(c.PostForm("picture")),
		Type:        currentProfile.Type, // System field: preserve existing value
	}

	// Private checkbox: only set to true if explicitly checked
	feedinfo.Private = c.PostForm("private") == "on"

	// Validate required fields
	if feedinfo.Id == "" {
		c.String(http.StatusBadRequest, "ID cannot be empty")
		return
	}
	if feedinfo.Name == "" {
		c.String(http.StatusBadRequest, "Name cannot be empty")
		return
	}

	profile, err := s.client.PostFeedinfo(ctx, feedinfo)
	if err != nil {
		log.Printf("PostFeedinfo error: %s", err)
		c.String(http.StatusBadRequest, "Failed to update profile: %s", err)
		return
	}

	// Redirect to the feed with the (possibly new) ID
	c.Redirect(http.StatusFound, "/feed/"+profile.Id)
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
