package server

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"context"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/gofrs/uuid"
	"github.com/patrickmn/go-cache"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	debug       bool
	client      pb.ApiClient
	worker      *pb.Worker
	secretKey   string
	httpclient  *http.Client
	cache       *cache.Cache
	media       *media.LocalStorage
	assets      embed.FS
	jsFile      string
	cssFile     string
	styleCssVer string
}

func NewServer(conn *grpc.ClientConn, assets embed.FS, cfg *util.Config, secretKey string, debug bool) *Server {
	c := pb.NewApiClient(conn)
	worker := &pb.Worker{
		Id: randhash(),
	}

	httpclient := &http.Client{
		Timeout: 30 * time.Second,
	}

	cacheStore := cache.New(5*time.Minute, 10*time.Minute)
	mfs := media.NewLocalStorage(cfg, 1024)

	s := &Server{
		debug:      debug,
		client:     c,
		worker:     worker,
		secretKey:  secretKey,
		httpclient: httpclient,
		cache:      cacheStore,
		media:      mfs,
		assets:     assets,
	}
	s.loadAssets()
	return s
}

// loadAssets resolves the content-hashed entry assets emitted by the Vite
// build (see httpd/app/static/manifest.json) so templates can reference them
// by their cache-proof URLs, and fingerprints the hand-written style.css.
func (s *Server) loadAssets() {
	// style.css is fingerprinted in every mode: a constant ?v=dev would be
	// cached forever by the CDN and hide edits during development.
	css, err := s.assets.ReadFile("static/css/style.css")
	if err == nil {
		s.styleCssVer = fingerprint(css)
	}

	if s.debug {
		return
	}

	raw, err := s.assets.ReadFile("static/manifest.json")
	if err != nil {
		log.Printf("manifest.json not found, asset URLs will be empty: %s", err)
	} else {
		s.jsFile, s.cssFile, err = parseAssetManifest(raw)
		if err != nil {
			log.Printf("can not parse manifest.json: %s", err)
		}
	}
}

func parseAssetManifest(raw []byte) (jsFile, cssFile string, err error) {
	var manifest map[string]struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return "", "", err
	}
	if entry, ok := manifest["src/index.jsx"]; ok {
		jsFile = entry.File
	}
	if entry, ok := manifest["style.css"]; ok {
		cssFile = entry.File
	}
	if jsFile == "" || cssFile == "" {
		return "", "", fmt.Errorf("manifest missing entry assets")
	}
	return jsFile, cssFile, nil
}

func fingerprint(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])[:8]
}

func DefaultTimeoutContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 1000*time.Millisecond)
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
		files, err := os.ReadDir("app/build/static/js")
		if err != nil {
			log.Println(err)
		}
		// Only the entry module gets a script tag; code-split chunks are
		// imported by the entry itself as ES modules.
		for _, fileName := range files {
			name := fileName.Name()
			if !fileName.IsDir() && strings.HasPrefix(name, "bundle-") && strings.HasSuffix(name, ".min.js") {
				jsFiles = append(jsFiles, name)
			}
		}
		data["jsFiles"] = jsFiles

		var cssFiles []string
		files, err = os.ReadDir("app/build/static/css")
		if err != nil {
			log.Println(err)
		}
		for _, fileName := range files {
			name := fileName.Name()
			if !fileName.IsDir() && strings.HasPrefix(name, "bundle-") && strings.HasSuffix(name, ".min.css") {
				cssFiles = append(cssFiles, name)
			}
		}
		data["cssFiles"] = cssFiles
	} else {
		data["jsFile"] = s.jsFile
		data["cssFile"] = s.cssFile
	}
	data["styleCssVer"] = s.styleCssVer
	c.HTML(code, name, data)
}

func (s *Server) renderFeed(c *gin.Context, data pongo2.Context) {
	requestedShare, _ := data["show_share"].(bool)
	data["show_share"] = showShareForUser(CurrentUserUuid(c), requestedShare)
	sanitizeFeedEntries(data["feed"].(*pb.Feed))

	if c.Request.Header.Get("X-Requested-With") == "XMLHttpRequest" ||
		c.Request.Header.Get("Content-Type") == "application/json" {
		c.JSON(200, data)
	} else {
		data["feed_body"] = ""
		encoded, _ := json.Marshal(data)
		data["appData"] = string(encoded)
		s.HTML(c, 200, "feed.html", data)
	}
}

func sanitizeFeedEntries(feed *pb.Feed) {
	for _, entry := range feed.Entries {
		if entry != nil {
			entry.Body = util.DefaultSanitize(entry.Body)
		}
	}
}

func showShareForUser(userUuid string, requested bool) bool {
	return userUuid != "" && requested
}

func (s *Server) CurrentUser(c *gin.Context) (*pb.Profile, error) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	uuid := CurrentUserUuid(c)
	if uuid == "" {
		return new(pb.Profile), nil
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

func (s *Server) ExpandCommentHandler(c *gin.Context) {
	s.expandEntry(c, true)
}

func (s *Server) ExpandLikeHandler(c *gin.Context) {
	s.expandEntry(c, false)
}

func (s *Server) expandEntry(c *gin.Context, comments bool) {
	uuid := c.Params.ByName("uuid")
	req := &pb.EntryRequest{Uuid: uuid}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	feed, err := s.client.FetchEntry(ctx, req)
	if RequestError(c, err) {
		return
	}
	entry, err := firstEntry(feed)
	if RequestError(c, err) {
		return
	}

	if !comments {
		c.JSON(200, entry.Likes)
		return
	}

	profile, _ := s.CurrentUser(c)
	graph, _ := s.CurrentGraph(c)
	entry.RebuildCommentsCommand(profile, graph)
	c.JSON(200, entry.Comments)
}

func firstEntry(feed *pb.Feed) (*pb.Entry, error) {
	if feed == nil || len(feed.Entries) == 0 || feed.Entries[0] == nil {
		return nil, status.Error(codes.NotFound, "entry not found")
	}
	return feed.Entries[0], nil
}

func (s *Server) LikeHandler(c *gin.Context) {
	s.updateLike(c, true)
}

func (s *Server) LikeDeleteHandler(c *gin.Context) {
	s.updateLike(c, false)
}

func (s *Server) updateLike(c *gin.Context, like bool) {
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
		Like:  like,
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
	var commentUUID uuid.UUID
	if id != "" {
		commentUUID, err = uuid.FromString(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "bad request"})
			return
		}
	} else {
		now := time.Now().UTC()
		comment.Date = now.Format(time.RFC3339)
		// Nanosecond name material: second precision collided when the same
		// user commented twice on one entry within a second.
		name := entryId + profile.Uuid + now.Format(time.RFC3339Nano)
		commentUUID = uuid.NewV5(uuid.NamespaceURL, name)
	}
	comment.Id = commentUUID.String()

	req := &pb.CommentRequest{
		Entry:    entryId,
		Comment:  comment,
		UserUuid: CurrentUserUuid(c),
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
	if err := c.MustBindWith(&form, binding.Form); err != nil {
		return
	}

	// Permission is enforced server-side by DeleteComment via user_uuid.
	profile, _ := s.CurrentUser(c)
	graph, _ := s.CurrentGraph(c)
	req := &pb.CommentDeleteRequest{
		Entry:    form.Entry,
		Comment:  form.Comment,
		UserUuid: CurrentUserUuid(c),
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
