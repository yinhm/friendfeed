package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/status"
)

// allUserGroups follows the bounded ListUserGroups cursor until exhaustion.
// Unlike the sidebar helper, this is the explicit full-list page, so it must
// not silently stop at the first RPC page or scan budget boundary.
func (s *Server) allUserGroups(ctx context.Context, userUUID string) ([]*pb.Profile, error) {
	groups := make([]*pb.Profile, 0)
	cursor := ""
	for {
		response, err := s.client.ListUserGroups(ctx, &pb.ListUserGroupsRequest{
			UserUuid: userUUID,
			Limit:    200,
			Cursor:   cursor,
		})
		if err != nil {
			return nil, err
		}
		groups = append(groups, response.Groups...)
		if response.NextCursor == "" {
			break
		}
		if response.NextCursor == cursor {
			return nil, fmt.Errorf("ListUserGroups returned a non-advancing cursor")
		}
		cursor = response.NextCursor
	}
	sort.Slice(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name)
	})
	return groups, nil
}

func prepareGroupPictures(groups []*pb.Profile) {
	for _, group := range groups {
		if group != nil {
			group.Picture = PictureOrDefault(group.Picture)
		}
	}
}

func (s *Server) UserGroupsPageHandler(c *gin.Context) {
	name := c.Params.ByName("name")
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	feed, err := s.client.FetchFeed(ctx, &pb.FeedRequest{
		Id: name, PageSize: 1, ViewerUuid: CurrentUserUuid(c),
	})
	if RequestError(c, err) {
		return
	}
	if location, renamed := renamedFeedLocationWithSuffix(name, feed, "groups", c.Request.URL.RawQuery); renamed {
		c.Redirect(http.StatusFound, location)
		return
	}
	if feed.Uuid != CurrentUserUuid(c) {
		c.HTML(http.StatusForbidden, "403.html", pongo2.Context{})
		return
	}
	groups, err := s.allUserGroups(ctx, feed.Uuid)
	if RequestError(c, err) {
		return
	}
	prepareGroupPictures(groups)
	s.HTML(c, http.StatusOK, "groups.html", pongo2.Context{
		"title":       "My groups",
		"heading":     "My groups",
		"groups":      groups,
		"show_create": true,
		"group_page":  "mine",
		"empty_text":  "You have not joined any groups yet.",
	})
}

func (s *Server) GroupDiscoveryPageHandler(c *gin.Context) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	response, err := s.client.ListGroups(ctx, &pb.ListGroupsRequest{
		Limit: 30, Cursor: c.Query("cursor"),
	})
	if RequestError(c, err) {
		return
	}
	prepareGroupPictures(response.Groups)
	s.HTML(c, http.StatusOK, "groups.html", pongo2.Context{
		"title":       "Groups",
		"heading":     "Groups",
		"groups":      response.Groups,
		"next_cursor": response.NextCursor,
		"show_create": CurrentUserId(c) != "",
		"group_page":  "discover",
		"empty_text":  "No groups are available yet.",
	})
}

// resolveGroupView maps a feed URL slug (profile ID) to its GroupView. Name
// resolution reuses FetchFeed's ID lookup (including the rename-map
// fallback); GetGroup then enforces Type == "group", returning NotFound for
// user/special profiles.
func (s *Server) resolveGroupView(c *gin.Context, name string) (*pb.GroupView, error) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	viewer := CurrentUserUuid(c)
	feed, err := s.client.FetchFeed(ctx, &pb.FeedRequest{
		Id:         name,
		PageSize:   1,
		ViewerUuid: viewer,
	})
	if err != nil {
		return nil, err
	}
	return s.client.GetGroup(ctx, &pb.GetGroupRequest{
		GroupUuid:  feed.Uuid,
		ViewerUuid: viewer,
	})
}

// canManageGroup mirrors the server-side manage_group rule for UI display:
// Group admin or super. GetGroup's is_admin does not cover supers, so the
// current user's IsSuper flag is checked separately; every POST still relies
// on the server's own authorization.
func canManageGroup(view *pb.GroupView, currentUser *pb.Profile) bool {
	if view == nil {
		return false
	}
	if view.IsAdmin {
		return true
	}
	return currentUser != nil && currentUser.IsSuper
}

// requireGroupManage resolves the :name Group and renders 404/403 when the
// current user may not manage it. Returns nil when the request was answered.
func (s *Server) requireGroupManage(c *gin.Context, name string) *pb.GroupView {
	view, err := s.resolveGroupView(c, name)
	if RequestError(c, err) {
		return nil
	}
	currentUser, err := s.CurrentUser(c)
	if RequestError(c, err) {
		return nil
	}
	if !canManageGroup(view, currentUser) {
		c.HTML(http.StatusForbidden, "403.html", pongo2.Context{})
		return nil
	}
	return view
}

func (s *Server) renderGroupSettings(c *gin.Context, view *pb.GroupView, errMsg string) {
	encoded, err := marshalPageBootstrap("group-settings", groupSettingsPageData{
		Group: groupFormViewFromProto(view.Group), Error: errMsg,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "Server error.")
		return
	}
	s.HTML(c, http.StatusOK, "app_shell.html", pongo2.Context{"title": "Group settings", "pageBootstrap": string(encoded)})
}

func (s *Server) GroupSettingsPageHandler(c *gin.Context) {
	view := s.requireGroupManage(c, c.Params.ByName("name"))
	if view == nil {
		return
	}
	s.renderGroupSettings(c, view, "")
}

func (s *Server) GroupSettingsHandler(c *gin.Context) {
	view := s.requireGroupManage(c, c.Params.ByName("name"))
	if view == nil {
		return
	}

	c.Request.ParseForm()
	// UpdateGroup overwrites all three fields, so submit the complete form
	// values, not only the ones the user touched.
	name := strings.TrimSpace(c.Request.Form.Get("name"))
	description := strings.TrimSpace(c.Request.Form.Get("description"))
	picture := strings.TrimSpace(c.Request.Form.Get("picture"))
	if name == "" {
		s.renderGroupSettings(c, view, "Group name is required")
		return
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	_, err := s.client.UpdateGroup(ctx, &pb.UpdateGroupRequest{
		ActorUuid:   CurrentUserUuid(c),
		GroupUuid:   view.Group.Uuid,
		Name:        name,
		Description: description,
		Picture:     picture,
	})
	if err != nil {
		// Keep the submitted values on the form and show the server's own
		// error message.
		view.Group.Name = name
		view.Group.Description = description
		view.Group.Picture = picture
		s.renderGroupSettings(c, view, status.Convert(err).Message())
		return
	}

	actor := CurrentUserUuid(c)
	s.cache.Delete("graph:" + actor)
	s.cache.Delete("profile:" + view.Group.Uuid)

	c.Redirect(http.StatusFound, "/feed/"+url.PathEscape(view.Group.Id))
}

func (s *Server) renderGroupMembers(c *gin.Context, view *pb.GroupView, errMsg string) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	resp, err := s.client.ListGroupMembers(ctx, &pb.ListGroupMembersRequest{
		GroupUuid:  view.Group.Uuid,
		Limit:      200,
		ViewerUuid: CurrentUserUuid(c),
	})
	if RequestError(c, err) {
		return
	}
	currentUser, err := s.CurrentUser(c)
	if RequestError(c, err) {
		return
	}
	for _, member := range resp.Members {
		if member != nil && member.Profile != nil {
			member.Profile.Picture = PictureOrDefault(member.Profile.Picture)
		}
	}
	manage := canManageGroup(view, currentUser)
	var requests []*pb.FollowRequestItem
	if manage {
		reqResp, err := s.client.ListFollowRequests(ctx, &pb.ListFollowRequestsRequest{
			ActorUuid: CurrentUserUuid(c),
			FeedUuid:  view.Group.Uuid,
			Limit:     200,
		})
		if RequestError(c, err) {
			return
		}
		for _, r := range reqResp.Requests {
			if r != nil && r.Requester != nil {
				r.Requester.Picture = PictureOrDefault(r.Requester.Picture)
			}
		}
		requests = reqResp.Requests
	}
	data := pongo2.Context{
		"title":                "Group members",
		"group":                view.Group,
		"members":              resp.Members,
		"requests":             requests,
		"has_more":             resp.NextCursor != "",
		"can_manage":           manage,
		"error":                errMsg,
		"feed_management_id":   view.Group.Id,
		"feed_management_page": "members",
		"group_members_url":    "/groups/" + url.PathEscape(view.Group.Id) + "/members",
	}
	if manage {
		data["group_settings_url"] = "/groups/" + url.PathEscape(view.Group.Id) + "/settings"
		data["manage_services_url"] = "/feed/" + url.PathEscape(view.Group.Id) + "/import"
	}
	s.HTML(c, 200, "group_members.html", data)
}

// GroupMembersPageHandler lists the Group's members. Any logged-in user may
// read the list; management buttons render only for admin/super.
func (s *Server) GroupMembersPageHandler(c *gin.Context) {
	view, err := s.resolveGroupView(c, c.Params.ByName("name"))
	if RequestError(c, err) {
		return
	}
	s.renderGroupMembers(c, view, "")
}

// GroupMemberActionHandler maps the promote/demote/remove form actions to
// the membership RPCs. Authorization is enforced server-side; failures (last
// admin, must-demote-first, ...) are redisplayed verbatim on the list page.
func (s *Server) GroupMemberActionHandler(c *gin.Context) {
	c.Request.ParseForm()
	action := c.Request.Form.Get("action")
	target := strings.TrimSpace(c.Request.Form.Get("target_uuid"))
	if target == "" {
		c.String(http.StatusBadRequest, "target_uuid is required")
		return
	}

	view, err := s.resolveGroupView(c, c.Params.ByName("name"))
	if RequestError(c, err) {
		return
	}

	req := &pb.GroupMembershipRequest{
		ActorUuid:  CurrentUserUuid(c),
		GroupUuid:  view.Group.Uuid,
		TargetUuid: target,
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	switch action {
	case "promote":
		_, err = s.client.AddGroupAdmin(ctx, req)
	case "demote":
		_, err = s.client.RemoveGroupAdmin(ctx, req)
	case "remove":
		_, err = s.client.RemoveGroupMember(ctx, req)
	case "approve":
		_, err = s.client.ApproveFollowRequest(ctx, &pb.FollowRequestAction{
			ActorUuid:  req.ActorUuid,
			FeedUuid:   req.GroupUuid,
			TargetUuid: req.TargetUuid,
		})
	case "reject":
		_, err = s.client.RejectFollowRequest(ctx, &pb.FollowRequestAction{
			ActorUuid:  req.ActorUuid,
			FeedUuid:   req.GroupUuid,
			TargetUuid: req.TargetUuid,
		})
	default:
		c.String(http.StatusBadRequest, "unknown action")
		return
	}
	if err != nil {
		s.renderGroupMembers(c, view, status.Convert(err).Message())
		return
	}

	c.Redirect(http.StatusFound, "/groups/"+url.PathEscape(view.Group.Id)+"/members")
}

func (s *Server) GroupDeleteHandler(c *gin.Context) {
	view := s.requireGroupManage(c, c.Params.ByName("name"))
	if view == nil {
		return
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	_, err := s.client.DeleteGroup(ctx, &pb.DeleteGroupRequest{
		ActorUuid: CurrentUserUuid(c),
		GroupUuid: view.Group.Uuid,
	})
	if err != nil {
		s.renderGroupSettings(c, view, status.Convert(err).Message())
		return
	}

	c.Redirect(http.StatusFound, "/")
}
