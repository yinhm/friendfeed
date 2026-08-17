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

func (s *Server) GroupsPageHandler(c *gin.Context) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	groups, err := s.allUserGroups(ctx, CurrentUserUuid(c))
	if RequestError(c, err) {
		return
	}
	for _, group := range groups {
		if group != nil {
			group.Picture = PictureOrDefault(group.Picture)
		}
	}
	s.HTML(c, http.StatusOK, "groups.html", pongo2.Context{
		"title":  "My groups",
		"groups": groups,
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
	s.HTML(c, 200, "group_settings.html", pongo2.Context{
		"title":        "Group settings",
		"group":        view.Group,
		"error":        errMsg,
		"form_action":  "/groups/" + url.PathEscape(view.Group.Id) + "/settings",
		"submit_label": "Save",
		"cancel_url":   "/feed/" + url.PathEscape(view.Group.Id),
	})
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
	s.HTML(c, 200, "group_members.html", pongo2.Context{
		"title":      "Group members",
		"group":      view.Group,
		"members":    resp.Members,
		"has_more":   resp.NextCursor != "",
		"can_manage": canManageGroup(view, currentUser),
		"error":      errMsg,
	})
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
