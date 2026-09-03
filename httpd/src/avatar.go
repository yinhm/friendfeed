package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) ProfileAvatarHandler(c *gin.Context) {
	actor := CurrentUserUuid(c)
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	profile, err := s.client.FetchProfile(ctx, &pb.ProfileRequest{Uuid: actor})
	if RequestError(c, err) {
		return
	}
	picture, err := s.pictureFromAvatarForm(c, profile.Picture, actor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid avatar upload"})
		return
	}
	updated, err := s.client.PostFeedinfo(ctx, &pb.Feedinfo{
		Uuid: profile.Uuid, Id: profile.Id, Name: profile.Name, Type: profile.Type,
		Private: profile.Private, Picture: picture, Description: profile.Description,
	})
	if RequestError(c, err) {
		return
	}
	s.cache.Delete("profile:" + actor)
	s.cache.Delete("graph:" + actor)
	c.JSON(http.StatusOK, gin.H{"picture": PictureOrDefault(updated.Picture)})
}

func (s *Server) GroupAvatarHandler(c *gin.Context) {
	actor := CurrentUserUuid(c)
	groupID := strings.TrimSpace(c.PostForm("group_id"))
	view, err := s.resolveGroupView(c, groupID)
	if RequestError(c, err) {
		return
	}
	currentUser, err := s.CurrentUser(c)
	if RequestError(c, err) {
		return
	}
	if !canManageGroup(view, currentUser) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}
	picture, err := s.pictureFromAvatarForm(c, view.Group.Picture, actor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid avatar upload"})
		return
	}
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	updated, err := s.client.UpdateGroup(ctx, &pb.UpdateGroupRequest{
		ActorUuid: actor, GroupUuid: view.Group.Uuid, Name: view.Group.Name,
		Description: view.Group.Description, Picture: picture,
	})
	if err != nil {
		if status.Code(err) == codes.PermissionDenied {
			c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			return
		}
		if RequestError(c, err) {
			return
		}
	}
	s.cache.Delete("graph:" + actor)
	s.cache.Delete("profile:" + view.Group.Uuid)
	c.JSON(http.StatusOK, gin.H{"picture": PictureOrDefault(updated.Picture)})
}
