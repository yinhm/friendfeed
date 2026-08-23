package server

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
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
	s.renderAccountPage(c, "profile", CurrentUserUuid(c))
}

// renderAccountPage serves the unified React account app (see account.html).
// Both /account/profile and /account/import load the same bundle; tab tells
// the app which panel to show first.
func (s *Server) renderAccountPage(c *gin.Context, tab, targetUUID string) {
	uuid := CurrentUserUuid(c)
	if uuid == "" {
		c.String(http.StatusUnauthorized, "please login first")
		return
	}

	profile, services, err := fetchAccountData(s.client, uuid, targetUUID)
	if err != nil {
		RequestError(c, err)
		return
	}

	// Hand the React app its data as JSON, mirroring the window.appData
	// pattern in feed.html.
	serviceMap := make(map[string]*pb.FeedService, len(services.Services))
	for _, service := range services.Services {
		serviceMap[service.Id] = service
	}
	accountJSON, err := json.Marshal(gin.H{
		"tab":      tab,
		"profile":  profile,
		"services": serviceMap,
		"states":   services.States,
		"target":   targetUUID,
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
func fetchAccountData(client pb.ApiClient, actorUUID, targetUUID string) (*pb.Profile, *pb.ListFeedServicesResponse, error) {
	req := &pb.ProfileRequest{Uuid: targetUUID}

	var wg sync.WaitGroup
	var profile *pb.Profile
	var services *pb.ListFeedServicesResponse
	var profileErr, servicesErr error

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
		services, servicesErr = client.ListFeedServices(ctx, &pb.ListFeedServicesRequest{
			ActorUuid: actorUUID, TargetFeedUuid: targetUUID,
		})
	}()
	wg.Wait()

	if profileErr != nil {
		return nil, nil, profileErr
	}
	if servicesErr != nil {
		return nil, nil, servicesErr
	}
	if services == nil {
		services = &pb.ListFeedServicesResponse{}
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
	s.renderAccountPage(c, "import", CurrentUserUuid(c))
}

func (s *Server) FeedImportPageHandler(c *gin.Context) {
	actor := CurrentUserUuid(c)
	name := c.Param("name")
	ctx, cancel := DefaultTimeoutContext()
	feed, err := s.client.FetchFeed(ctx, &pb.FeedRequest{
		Id: name, PageSize: 1, ViewerUuid: actor,
	})
	cancel()
	if RequestError(c, err) {
		return
	}
	if location, renamed := renamedFeedLocationWithSuffix(name, feed, "import", c.Request.URL.RawQuery); renamed {
		c.Redirect(http.StatusFound, location)
		return
	}

	ctx, cancel = DefaultTimeoutContext()
	services, err := s.client.ListFeedServices(ctx, &pb.ListFeedServicesRequest{
		ActorUuid: actor, TargetFeedUuid: feed.Uuid,
	})
	cancel()
	if RequestError(c, err) {
		return
	}
	if services == nil {
		services = &pb.ListFeedServicesResponse{}
	}
	serviceMap := make(map[string]*pb.FeedService, len(services.Services))
	for _, service := range services.Services {
		if service != nil {
			serviceMap[service.Id] = service
		}
	}
	encoded, err := json.Marshal(gin.H{
		"services": serviceMap,
		"states":   services.States,
		"target":   feed.Uuid,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to encode feed services")
		return
	}
	data := pongo2.Context{
		"title":                "Import Services",
		"feed":                 feed,
		"feedImportData":       string(encoded),
		"feed_management_id":   feed.Id,
		"feed_management_page": "import",
		"manage_services_url":  "/feed/" + url.PathEscape(feed.Id) + "/import",
	}
	if feed.Type == "group" {
		data["group_settings_url"] = "/groups/" + url.PathEscape(feed.Id) + "/settings"
		data["group_members_url"] = "/groups/" + url.PathEscape(feed.Id) + "/members"
	}
	s.HTML(c, http.StatusOK, "feed_import.html", data)
}

func (s *Server) AddFeedServiceHandler(c *gin.Context) {
	actor := CurrentUserUuid(c)
	if actor == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "please login first"})
		return
	}
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	binding, err := s.client.AddFeedService(ctx, &pb.AddFeedServiceRequest{
		ActorUuid: actor, TargetFeedUuid: strings.TrimSpace(c.PostForm("target_uuid")),
		Kind: "web_feed", Url: strings.TrimSpace(c.PostForm("url")),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": status.Convert(err).Message()})
		return
	}
	c.JSON(http.StatusOK, binding)
}

func (s *Server) FeedServiceActionHandler(c *gin.Context) {
	actor := CurrentUserUuid(c)
	if actor == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "please login first"})
		return
	}
	target := strings.TrimSpace(c.PostForm("target_uuid"))
	serviceID := c.Param("service")
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	switch c.Param("action") {
	case "enable", "disable":
		binding, err := s.client.SetFeedServiceEnabled(ctx, &pb.SetFeedServiceEnabledRequest{
			ActorUuid: actor, TargetFeedUuid: target, ServiceId: serviceID,
			Enabled: c.Param("action") == "enable",
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": status.Convert(err).Message()})
			return
		}
		c.JSON(http.StatusOK, binding)
	case "refresh":
		_, err := s.client.RefreshFeedService(ctx, &pb.RefreshFeedServiceRequest{
			ActorUuid: actor, TargetFeedUuid: target, ServiceId: serviceID,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": status.Convert(err).Message()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"queued": serviceID})
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown FeedService action"})
	}
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
	target := strings.TrimSpace(c.Query("target"))
	if target == "" {
		target = uuid
	}
	req := &pb.RemoveFeedServiceRequest{
		ActorUuid: uuid, TargetFeedUuid: target, ServiceId: service,
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
	} else if fResp.Requested {
		feed.Commands = []string{"requested"}
	} else {
		feed.Commands = []string{"follow"}
	}
}
