package server

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"golang.org/x/exp/utf8string"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/contrib/cache"
	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/golang/protobuf/proto"
	"github.com/yinhm/friendfeed/ff"
	pb "github.com/yinhm/friendfeed/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type Server struct {
	client     pb.ApiClient
	worker     *pb.Worker
	secretKey  string
	httpclient *http.Client
	cache      *cache.InMemoryStore
}

func NewServer(conn *grpc.ClientConn, secretKey string) *Server {
	c := pb.NewApiClient(conn)
	worker := &pb.Worker{
		Id: randhash(),
	}

	httpclient := &http.Client{
		Timeout: 30 * time.Second,
	}

	cacheStore := cache.NewInMemoryStore(time.Second)

	return &Server{
		client:     c,
		worker:     worker,
		secretKey:  secretKey,
		httpclient: httpclient,
		cache:      cacheStore,
	}
}

func DefaultTimeoutContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 500*time.Millisecond)
}

func (s *Server) HTML(c *gin.Context, code int, name string, data pongo2.Context) {
	profile, err := s.CurrentUser(c)
	if err != nil {
		c.String(http.StatusInternalServerError, "error on fetch user")
		return
	}
	if profile.Uuid != "" {
		data["current_user"] = profile
	}
	c.HTML(code, name, data)
}

func (s *Server) CurrentUser(c *gin.Context) (*pb.Profile, error) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	profile := new(pb.Profile)
	uuid := CurrentUserUuid(c)
	if uuid != "" {
		cacheKey := "profile:" + uuid
		err := s.cache.Get(cacheKey, profile)
		if err != nil {
			profile, err = s.client.FetchProfile(ctx, &pb.ProfileRequest{uuid})
			if err != nil {
				return nil, err
			}
			if err := s.cache.Set(cacheKey, *profile, 15*time.Minute); err != nil {
				return nil, err
			}
		}
	}
	return profile, nil
}

func (s *Server) CurrentFeedinfo(c *gin.Context) (*pb.Feedinfo, error) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	feedinfo := new(pb.Feedinfo)
	uuid := CurrentUserUuid(c)
	if uuid != "" {
		cacheKey := "feedinfo:" + uuid
		err := s.cache.Get(cacheKey, feedinfo)
		if err != nil {
			req := &pb.ProfileRequest{Uuid: uuid}
			feedinfo, err = s.client.FetchFeedinfo(ctx, req)
			if err != nil {
				return nil, err
			}
			if err := s.cache.Set(cacheKey, *feedinfo, 15*time.Minute); err != nil {
				return nil, err
			}
		}
	}
	return feedinfo, nil
}

func (s *Server) CurrentGraph(c *gin.Context) (*pb.Graph, error) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	graph := new(pb.Graph)
	uuid := CurrentUserUuid(c)
	if uuid != "" {
		cacheKey := "graph:" + uuid
		err := s.cache.Get(cacheKey, graph)
		if err != nil {
			req := &pb.ProfileRequest{Uuid: uuid}
			graph, err = s.client.FetchGraph(ctx, req)
			if err != nil {
				return nil, err
			}
			if err := s.cache.Set(cacheKey, *graph, 15*time.Minute); err != nil {
				return nil, err
			}
		}
	}
	return graph, nil
}

func (s *Server) feedReadable(c *gin.Context, feedId string) bool {
	user, err := s.CurrentUser(c)
	if err != nil {
		return false
	}
	if user.Id == feedId {
		return true
	}

	graph, err := s.CurrentGraph(c)
	if err != nil || graph == nil {
		return false
	}
	if _, ok := graph.Subscriptions[feedId]; ok {
		return true
	}

	return false
}

func (s *Server) FetchFeed(c *gin.Context, req proto.Message) (feed *pb.Feed, err error) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	switch req.(type) {
	case *pb.FeedRequest:
		feed, err = s.client.FetchFeed(ctx, req.(*pb.FeedRequest))
	case *pb.EntryRequest:
		feed, err = s.client.FetchEntry(ctx, req.(*pb.EntryRequest))
	}
	if err != nil {
		return nil, err
	}
	profile, err := s.CurrentUser(c)
	if err != nil {
		return nil, err
	}
	graph, err := s.CurrentGraph(c)
	if err != nil {
		return nil, err
	}
	for _, e := range feed.Entries {
		e.RebuildCommand(profile, graph)
	}
	return feed, nil
}

func (s *Server) AccountHandler(c *gin.Context) {
}

func (s *Server) ImportHandler(c *gin.Context) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	uuid := CurrentUserUuid(c)
	if uuid == "" {
		c.String(http.StatusBadRequest, "no profile yet")
		return
	}
	req := &pb.ProfileRequest{Uuid: uuid}
	graph, err := s.client.FetchGraph(ctx, req)
	if err != nil {
		c.String(http.StatusBadRequest, "no profile yet")
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

func (s *Server) HomeHandler(c *gin.Context) {
	start := ParseStart(c.Request)
	req := &pb.FeedRequest{
		Id:       "public",
		Start:    int32(start),
		PageSize: 30,
	}

	feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	prevStart := req.Start - req.PageSize
	if prevStart < 0 {
		prevStart = 0
	}
	data := pongo2.Context{
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"prev_start":  prevStart,
		"next_start":  req.Start + req.PageSize,
		"show_paging": true,
	}
	s.HTML(c, 200, "feed.html", data)
}

func (s *Server) FeedHandler(c *gin.Context) {
	feedname := c.Params.ByName("name")
	if feedname == "" {
		feedname = "home"
	}
	start := ParseStart(c.Request)
	req := &pb.FeedRequest{
		Id:       feedname,
		Start:    int32(start),
		PageSize: 30,
	}
	feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}
	if feed.Private && !s.feedReadable(c, feed.Id) {
		c.HTML(http.StatusForbidden, "403.html", pongo2.Context{})
		return
	}

	showHeader := feed.Id != "Home" && !strings.HasPrefix(feed.Id, "e/")
	showShare := feed.Id == "Home" || contains(feed.Commands, "post")
	showDirect := contains(feed.Commands, "dm")
	prevStart := req.Start - req.PageSize
	if prevStart < 0 {
		prevStart = 0
	}
	data := pongo2.Context{
		"show_header": showHeader,
		"show_share":  showShare,
		"show_direct": showDirect,
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"ff_username": "me",
		"prev_start":  prevStart,
		"next_start":  req.Start + req.PageSize,
		"show_paging": true,
	}
	s.HTML(c, 200, "feed.html", data)
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (s *Server) EntryHandler(c *gin.Context) {
	uuid := c.Params.ByName("uuid")
	req := &pb.EntryRequest{uuid}
	feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	entry := feed.Entries[0]
	rawBody := utf8string.NewString(entry.RawBody)
	title := rawBody.String()
	if rawBody.RuneCount() > 42 {
		title = rawBody.Slice(0, 42)
	}
	data := pongo2.Context{
		"title":       title,
		"name":        entry.From.Name,
		"feed":        feed,
		"show_paging": false,
	}
	s.HTML(c, 200, "feed.html", data)
}

func (s *Server) EntryCommentHandler(c *gin.Context) {
	uuid := c.Params.ByName("uuid")
	req := &pb.EntryRequest{uuid}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	feed, err := s.client.FetchEntry(ctx, req)
	if RequestError(c, err) {
		return
	}

	comments := feed.Entries[0].Comments
	c.JSON(200, gin.H{"comments": comments})
}

func (s *Server) LikeHandler(c *gin.Context) {
	c.Request.ParseForm()
	entryId := c.Request.Form.Get("entry")
	if entryId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "bad request"})
		return
	}

	uuid := CurrentUserUuid(c)
	req := &pb.LikeRequest{
		Entry: entryId,
		User:  uuid,
		Like:  true,
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	_, err := s.client.LikeEntry(ctx, req)
	if RequestError(c, err) {
		return
	}

	c.JSON(200, gin.H{"success": true})
}

func (s *Server) LikeDeleteHandler(c *gin.Context) {
	c.Request.ParseForm()
	entryId := c.Request.Form.Get("entry")
	if entryId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "bad request"})
		return
	}

	uuid := CurrentUserUuid(c)
	req := &pb.LikeRequest{
		Entry: entryId,
		User:  uuid,
		Like:  false,
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	_, err := s.client.LikeEntry(ctx, req)
	if RequestError(c, err) {
		return
	}

	c.JSON(200, gin.H{"success": true})
}

func (s *Server) CommentHandler(c *gin.Context) {
	c.Request.ParseForm()
	entryId := c.Request.Form.Get("entry")
	body := c.Request.Form.Get("body")
	if entryId == "" || body == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "bad request"})
		return
	}

	uuid := CurrentUserUuid(c)
	req := &pb.CommentRequest{
		Entry: entryId,
		User:  uuid,
		Body:  body,
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	_, err := s.client.CommentEntry(ctx, req)
	if RequestError(c, err) {
		return
	}

	c.JSON(200, gin.H{"success": true})
}

func RequestError(c *gin.Context, err error) bool {
	if err != nil {
		if grpc.Code(err) == codes.DeadlineExceeded {
			c.String(http.StatusServiceUnavailable, "Server busy, try later.")
		} else {
			// TODO: hacky error code
			if err.Error() == "rpc error: code = 2 desc = \"404\"" {
				c.HTML(404, "404.html", pongo2.Context{})
			} else {
				msg := "Server error, user may not exists or not mirrored."
				c.String(http.StatusInternalServerError, msg)
			}
		}
		return true
	}
	return false
}

func randhash() string {
	randbytes := make([]byte, 4)
	rand.Read(randbytes)

	h := sha1.New()
	h.Write(randbytes)
	return hex.EncodeToString(h.Sum(nil))[:12]
}
