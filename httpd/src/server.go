package server

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/utf8string"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
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
}

func NewServer(conn *grpc.ClientConn, secretKey string) *Server {
	c := pb.NewApiClient(conn)
	worker := &pb.Worker{
		Id: randhash(),
	}

	httpclient := &http.Client{
		Timeout: 30 * time.Second,
	}

	return &Server{
		client:     c,
		worker:     worker,
		secretKey:  secretKey,
		httpclient: httpclient,
	}
}

func DefaultTimeoutContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 500*time.Millisecond)
}

func (s *Server) AccountHandler(c *gin.Context) {
}

func (s *Server) ImportHandler(c *gin.Context) {
	data := pongo2.Context{
		"title": "import your",
	}
	c.HTML(200, "import.html", data)
}

func (s *Server) HTML(c *gin.Context, code int, name string, data pongo2.Context) {
	sess := sessions.Default(c)
	uuid := sess.Get("uuid")
	if uuid != nil && uuid.(string) != "" {
		ctx, cancel := DefaultTimeoutContext()
		defer cancel()

		profile, err := s.client.FetchProfile(ctx, &pb.ProfileRequest{uuid.(string)})
		if err != nil {
			c.String(http.StatusInternalServerError, "error on fetch user")
			return
		}

		data["current_user"] = profile
	}

	c.HTML(code, name, data)
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
	req := &pb.FeedRequest{
		Id:       "public",
		Start:    0,
		PageSize: 30,
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	feed, err := s.client.FetchFeed(ctx, req)
	if RequestError(c, err) {
		return
	}

	data := pongo2.Context{
		"title": feed.Id,
		"name":  feed.Id,
		"feed":  feed,
	}
	s.HTML(c, 200, "feed.html", data)
}

func (s *Server) FeedHandler(c *gin.Context) {
	feedname := c.Params.ByName("name")
	if feedname == "" {
		feedname = "home"
	}

	query := c.Request.URL.Query()
	startS := query.Get("start")
	if startS == "" {
		startS = "0"
	}
	start, _ := strconv.Atoi(startS)
	if start > 20000 {
		start = 20000
	}

	req := &pb.FeedRequest{
		Id:       feedname,
		Start:    int32(start),
		PageSize: 30,
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	feed, err := s.client.FetchFeed(ctx, req)
	if RequestError(c, err) {
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

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	feed, err := s.client.FetchEntry(ctx, req)
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
