package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/status"
)

func (s *Server) AccountHandler(c *gin.Context) {
	c.Redirect(http.StatusFound, "/account/profile")
}

func (s *Server) AccountProfileHandler(c *gin.Context) {
	s.renderAccountPage(c, "profile")
}

// renderAccountPage serves the unified React account app (see account.html).
// Both /account/profile and /account/import load the same bundle; tab tells
// the app which panel to show first.
func (s *Server) renderAccountPage(c *gin.Context, tab string) {
	uuid := CurrentUserUuid(c)
	if uuid == "" {
		c.String(http.StatusUnauthorized, "please login first")
		return
	}

	profile, services, err := fetchAccountData(s.client, uuid)
	if err != nil {
		RequestError(c, err)
		return
	}

	// Hand the React app its data as JSON, mirroring the window.appData
	// pattern in feed.html.
	accountJSON, err := json.Marshal(gin.H{
		"tab":      tab,
		"profile":  profile,
		"services": services,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to encode account data")
		return
	}
	data := pongo2.Context{
		"title":       "Account",
		"profile":     profile,
		"accountData": string(accountJSON),
	}
	s.HTML(c, 200, "account.html", data)
}

// fetchAccountData loads the profile and the services graph in parallel,
// each with its own timeout: the unified account page needs both, and two
// serial calls sharing one deadline would let a slow first call starve the
// second.
func fetchAccountData(client pb.ApiClient, userUuid string) (*pb.Profile, map[string]*pb.FeedService, error) {
	req := &pb.ProfileRequest{Uuid: userUuid}

	var wg sync.WaitGroup
	var profile *pb.Profile
	var graph *pb.Graph
	var profileErr, graphErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		ctx, cancel := DefaultTimeoutContext()
		defer cancel()
		profile, profileErr = client.FetchProfile(ctx, req)
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := DefaultTimeoutContext()
		defer cancel()
		graph, graphErr = client.FetchGraph(ctx, req)
	}()
	wg.Wait()

	if profileErr != nil {
		return nil, nil, profileErr
	}
	if graphErr != nil {
		return nil, nil, graphErr
	}

	services := graph.Services
	if services == nil {
		services = map[string]*pb.FeedService{}
	}
	return profile, services, nil
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

	// CurrentUser/GraphFrom cache profile and graph for 5 minutes; a stale
	// cached id would mis-evaluate RebuildCommand/RebuildCommentsCommand
	// (like state, comment edit commands) after an id rename.
	s.cache.Delete("profile:" + uuid)
	s.cache.Delete("graph:" + uuid)

	c.JSON(200, profile)
}

func (s *Server) ImportHandler(c *gin.Context) {
	s.renderAccountPage(c, "import")
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": "please login first"})
		return
	}
	req := &pb.RemoveFeedServiceRequest{
		ActorUuid: uuid, TargetFeedUuid: uuid, ServiceId: service,
	}
	_, err := s.client.RemoveFeedService(ctx, req)
	if err != nil {
		log.Printf("Error on deleting: %s, %s", uuid, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": status.Convert(err).Message()})
		return
	}
	// The React account app expects JSON; legacy direct hits get a redirect.
	if c.Request.Header.Get("X-Requested-With") == "XMLHttpRequest" ||
		c.Request.Header.Get("Content-Type") == "application/json" {
		c.JSON(200, gin.H{"deleted": service})
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
