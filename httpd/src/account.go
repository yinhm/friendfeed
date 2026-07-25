package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/status"
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

	// The page is a React app (see account_profile.html); hand it the
	// profile as JSON, mirroring the window.appData pattern in feed.html.
	profileJSON, err := json.Marshal(profile)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to encode profile")
		return
	}
	data := pongo2.Context{
		"title":       "Edit Profile",
		"profile":     profile,
		"profileData": string(profileJSON),
	}
	s.HTML(c, 200, "account_profile.html", data)
}

func (s *Server) AccountProfileUpdateHandler(c *gin.Context) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	uuid := CurrentUserUuid(c)
	if uuid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "please login first"})
		return
	}

	// Fetch current profile to preserve system fields
	req := &pb.ProfileRequest{Uuid: uuid}
	currentProfile, err := s.client.FetchProfile(ctx, req)
	if err != nil {
		log.Printf("FetchProfile error: %s", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch current profile"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID cannot be empty"})
		return
	}
	if feedinfo.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name cannot be empty"})
		return
	}

	profile, err := s.client.PostFeedinfo(ctx, feedinfo)
	if err != nil {
		log.Printf("PostFeedinfo error: %s", err)
		// Surface the RPC message (e.g. "ID is already taken") without the
		// "rpc error: code = ... desc = ..." wrapper.
		msg := status.Convert(err).Message()
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}

	c.JSON(200, profile)
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
