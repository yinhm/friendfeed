package server

import (
	"crypto/rand"
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/gofrs/uuid"
	"github.com/patrickmn/go-cache"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	debug      bool
	client     pb.ApiClient
	worker     *pb.Worker
	secretKey  string
	httpclient *http.Client
	cache      *cache.Cache
	assets     embed.FS
}

func NewServer(conn *grpc.ClientConn, assets embed.FS, secretKey string, debug bool) *Server {
	c := pb.NewApiClient(conn)
	worker := &pb.Worker{
		Id: randhash(),
	}

	httpclient := &http.Client{
		Timeout: 30 * time.Second,
	}

	cacheStore := cache.New(5*time.Minute, 10*time.Minute)

	return &Server{
		debug:      debug,
		client:     c,
		worker:     worker,
		secretKey:  secretKey,
		httpclient: httpclient,
		cache:      cacheStore,
		assets:     assets,
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
	data["dev"] = s.debug

	if s.debug {
		var jsFiles []string
		files, err := ioutil.ReadDir("app/build/static/js")
		if err != nil {
			log.Println(err)
		}
		for _, fileName := range files {
			if !fileName.IsDir() && strings.HasSuffix(fileName.Name(), "js") {
				jsFiles = append(jsFiles, fileName.Name())
			}
		}
		data["jsFiles"] = jsFiles
	}
	c.HTML(code, name, data)
}

func (s *Server) renderFeed(c *gin.Context, data pongo2.Context) {
	if c.Request.Header.Get("X-Requested-With") == "XMLHttpRequest" ||
		c.Request.Header.Get("Content-Type") == "application/json" {
		c.JSON(200, data)
	} else {
		data["feed_body"] = ""
		encoded, _ := json.Marshal(data)
		data["appData"] = string(encoded)
		s.HTML(c, 200, "_feed.html", data)
	}
}

func (s *Server) CurrentUser(c *gin.Context) (*pb.Profile, error) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	uuid := CurrentUserUuid(c)
	if uuid == "" {
		return nil, nil
	}

	cacheKey := "profile:" + uuid
	v, found := s.cache.Get(cacheKey)
	if !found {
		profile, err := s.client.FetchProfile(ctx, &pb.ProfileRequest{Uuid: uuid})
		if err != nil {
			return nil, err
		}
		s.cache.Set(cacheKey, profile, cache.DefaultExpiration)
		return profile, nil
	}
	return v.(*pb.Profile), nil
}

// func (s *Server) CurrentFeedinfo(c *gin.Context) (*pb.Feedinfo, error) {
// 	ctx, cancel := DefaultTimeoutContext()
// 	defer cancel()

// 	feedinfo := new(pb.Feedinfo)
// 	uuid := CurrentUserUuid(c)
// 	if uuid != "" {
// 		cacheKey := "feedinfo:" + uuid
// 		err := s.cache.Get(cacheKey, feedinfo)
// 		if err != nil {
// 			req := &pb.ProfileRequest{Uuid: uuid}
// 			feedinfo, err = s.client.FetchFeedinfo(ctx, req)
// 			if err != nil {
// 				return nil, err
// 			}
// 			if err := s.cache.Set(cacheKey, *feedinfo, 15*time.Minute); err != nil {
// 				return nil, err
// 			}
// 		}
// 	}
// 	return feedinfo, nil
// }

func (s *Server) CurrentGraph(c *gin.Context) (*pb.Graph, error) {
	return s.GraphFrom(CurrentUserUuid(c))
}

func (s *Server) feedReadable(c *gin.Context, feedUuid string) bool {
	user, err := s.CurrentUser(c)
	if err != nil {
		return false
	}
	if user.Uuid == feedUuid {
		return true
	}

	graph, err := s.CurrentGraph(c)
	if err != nil || graph == nil {
		return false
	}
	// if _, ok := graph.Subscriptions[feedId]; ok {
	// 	return true
	// }

	return false
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (s *Server) ExpandCommentHandler(c *gin.Context) {
	uuid := c.Params.ByName("uuid")
	req := &pb.EntryRequest{Uuid: uuid}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	feed, err := s.client.FetchEntry(ctx, req)
	if RequestError(c, err) {
		return
	}

	profile, _ := s.CurrentUser(c)
	graph, _ := s.CurrentGraph(c)
	feed.Entries[0].RebuildCommentsCommand(profile, graph)
	c.JSON(200, feed.Entries[0].Comments)
}

func (s *Server) ExpandLikeHandler(c *gin.Context) {
	uuid := c.Params.ByName("uuid")
	req := &pb.EntryRequest{Uuid: uuid}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	feed, err := s.client.FetchEntry(ctx, req)
	if RequestError(c, err) {
		return
	}
	c.JSON(200, feed.Entries[0].Likes)
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

	entry, err := s.client.LikeEntry(ctx, req)
	if RequestError(c, err) {
		return
	}

	entry.FormatLikes(int32(0))
	c.JSON(200, entry.Likes)
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

	entry, err := s.client.LikeEntry(ctx, req)
	if RequestError(c, err) {
		return
	}

	entry.FormatLikes(int32(0))
	c.JSON(200, entry.Likes)
}

// /a/comment
func (s *Server) CommentHandler(c *gin.Context) {
	c.Request.ParseForm()
	id := c.Request.Form.Get("id")
	entryId := c.Request.Form.Get("entry")
	rawBody := c.Request.Form.Get("body")
	if entryId == "" || rawBody == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "bad request"})
		return
	}

	body := util.DefaultSanitize(rawBody)
	body = util.EntityToLink(body)

	profile, _ := s.CurrentUser(c)
	from := &pb.Feed{
		Id:   profile.Id,
		Name: profile.Name,
		Type: profile.Type,
	}
	comment := &pb.Comment{
		Body:    body,
		RawBody: rawBody,
		From:    from,
	}

	var err error
	var uuid1 uuid.UUID
	if id != "" {
		uuid1, err = uuid.FromString(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "bad request"})
			return
		}
	} else {
		comment.Date = time.Now().UTC().Format(time.RFC3339)
		name := entryId + profile.Uuid + comment.Date
		uuid1 = uuid.NewV5(uuid.NamespaceURL, name)
	}
	comment.Id = uuid1.String()

	req := &pb.CommentRequest{
		Entry:   entryId,
		Comment: comment,
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	_, err = s.client.CommentEntry(ctx, req)
	if RequestError(c, err) {
		return
	}

	comment.Commands = []string{"edit", "delete"}
	c.JSON(200, comment)
}

func (s *Server) CommentDeleteHandler(c *gin.Context) {
	var form struct {
		Entry   string `form:"entry" binding:"required"`
		Comment string `form:"comment" binding:"required"`
	}
	c.MustBindWith(&form, binding.Form)

	// TODO: check perm
	profile, _ := s.CurrentUser(c)
	graph, _ := s.CurrentGraph(c)
	req := &pb.CommentDeleteRequest{
		Entry:   form.Entry,
		Comment: form.Comment,
		User:    profile.Id,
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	entry, err := s.client.DeleteComment(ctx, req)
	if RequestError(c, err) {
		return
	}

	entry.FormatComments(int32(0))
	entry.RebuildCommentsCommand(profile, graph)
	c.JSON(200, entry.Comments)
}

// Follow
func (s *Server) FollowHandler(c *gin.Context) {
	c.Request.ParseForm()
	feedUuid := c.Request.Form.Get("feed_uuid")
	action := c.Request.Form.Get("action")
	if feedUuid == "" || action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "bad request"})
		return
	}

	uuid := CurrentUserUuid(c)
	req := &pb.FollowRequest{
		ProfileUuid: uuid,
		FeedUuid:    feedUuid,
		Action:      action,
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	entry, err := s.client.GraphFollow(ctx, req)
	if RequestError(c, err) {
		return
	}

	c.JSON(200, entry.Followed)
}

func RequestError(c *gin.Context, err error) bool {
	if err != nil {
		errStatus, _ := status.FromError(err)
		if codes.Unavailable == errStatus.Code() {
			c.String(http.StatusServiceUnavailable, "Server Unavailable.")
		} else if codes.DeadlineExceeded == errStatus.Code() {
			c.String(http.StatusServiceUnavailable, "Server busy, try later.")
		} else if codes.NotFound == errStatus.Code() {
			c.HTML(404, "404.html", pongo2.Context{})
		} else {
			msg := "Server error."
			c.String(http.StatusInternalServerError, msg)
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
