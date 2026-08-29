package server

import (
	"net/http"
	"sort"
	"strings"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
)

type profileRelationsPageData struct {
	Profile  groupFormView   `json:"profile"`
	Relation string          `json:"relation"`
	Profiles []groupFormView `json:"profiles"`
}

// ProfileRelationsHandler renders one complete side of a user Profile's
// social graph. It intentionally calls FetchGraph only for these explicit
// pages; ordinary Feed reads do not pay for relationship scans.
func (s *Server) ProfileRelationsHandler(relation string) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Params.ByName("name")
		ctx, cancel := DefaultTimeoutContext()
		defer cancel()

		feed, err := s.client.FetchFeed(ctx, &pb.FeedRequest{
			Id: name, PageSize: 1, ViewerUuid: CurrentUserUuid(c),
		})
		if RequestError(c, err) {
			return
		}
		if location, renamed := renamedFeedLocationWithSuffix(name, feed, relation, c.Request.URL.RawQuery); renamed {
			c.Redirect(http.StatusFound, location)
			return
		}
		if feed.Type != "user" {
			c.HTML(http.StatusNotFound, "404.html", pongo2.Context{})
			return
		}

		graph, err := s.client.FetchGraph(ctx, &pb.ProfileRequest{Uuid: feed.Uuid})
		if RequestError(c, err) {
			return
		}
		profiles := graph.Following
		if relation == "followers" {
			profiles = graph.Followers
		}
		items := make([]*pb.Profile, 0, len(profiles))
		for _, profile := range profiles {
			if profile != nil {
				profile.Picture = PictureOrDefault(profile.Picture)
				items = append(items, profile)
			}
		}
		sort.Slice(items, func(i, j int) bool {
			left, right := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
			if left == right {
				return items[i].Id < items[j].Id
			}
			return left < right
		})

		encoded, err := marshalPageBootstrap("profile-relations", profileRelationsPageData{
			Profile:  groupFormView{ID: feed.Id, Name: feed.Name, Picture: PictureOrDefault(feed.Picture), Private: feed.Private},
			Relation: relation, Profiles: groupFormViewsFromProto(items),
		})
		if err != nil {
			c.String(http.StatusInternalServerError, "Server error.")
			return
		}
		title := "Following"
		if relation == "followers" {
			title = "Followers"
		}
		s.HTML(c, http.StatusOK, "app_shell.html", pongo2.Context{
			"title": title + " · " + feed.Name, "pageBootstrap": string(encoded),
		})
	}
}
