package server

import (
	"log"
	"net/http"
	"time"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/ff"
	pb "github.com/yinhm/friendfeed/proto"
)

func (s *Server) AccountHandler(c *gin.Context) {
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

func (s *Server) FriendFeedImportHandler(c *gin.Context) {
	c.Request.ParseForm()

	username := c.Request.Form.Get("username")
	remoteKey := c.Request.Form.Get("remote_key")
	if username == "" {
		c.String(400, "Unknown feed")
		return
	}

	// group feed not supported
	apiv1 := ff.NewV1Client(s.httpclient, username, remoteKey)
	v1profile, resp, err := apiv1.V1Profile(username, "user")
	if err != nil {
		c.String(resp.StatusCode, err.Error())
		return
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	sess := sessions.Default(c)
	userId := sess.Get("user_id").(string)
	provider := sess.Get("provider").(string)

	oauthUser := &pb.OAuthUser{
		Uuid:      v1profile.Id,
		UserId:    userId,
		Provider:  provider,
		RemoteKey: remoteKey,
	}

	_, err = s.client.BindUserFeed(ctx, oauthUser)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	targetId := username
	job := &pb.FeedJob{
		Id:        username,
		RemoteKey: remoteKey,
		TargetId:  targetId,
		Start:     0,
		PageSize:  100,
		Created:   time.Now().Unix(),
		Updated:   time.Now().Unix(),
	}
	_, err = s.client.EnqueJob(ctx, job)
	if err != nil {
		c.String(http.StatusInternalServerError, "server error")
		return
	}

	http.Redirect(c.Writer, c.Request, "/feed/"+username, http.StatusFound)
}
