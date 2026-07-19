package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
	server "github.com/yinhm/friendfeed/httpd/src"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/google"
	"github.com/markbates/goth/providers/twitter"
)

//go:embed static templates
var assetsFS embed.FS

var options struct {
	Debug      bool
	Rpc        string
	Port       uint
	SecretKey  string
	ConfigFile string
}

func init() {
	flag.BoolVar(&options.Debug, "d", false, "Enable debug")
	flag.StringVar(&options.Rpc, "rpc", "localhost:8901", "Rpc Server Address")
	flag.UintVar(&options.Port, "p", 8080, "HTTP server listen port")
	flag.StringVar(&options.SecretKey, "s", "randombitsreplacedlkjsa", "Key used to encryption cookies")
	flag.StringVar(&options.ConfigFile, "c", "/srv/ffdb/config.json", "Config file")

	// babel.Init(runtime.NumCPU())
}

func waitShutdown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Println("shutdown webserver...")
}

func NotFoundHandler(c *gin.Context) {
	ctx := pongo2.Context{
		"title": "Frienfeed",
		"name":  "404 not found",
	}
	c.HTML(404, "404.html", ctx)
}

func FaviconHandler(c *gin.Context) {
	favicon, err := assetsFS.ReadFile("static/favicon.ico")
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusOK, "image/x-icon", favicon)
}

func embeddedAssetHandler(assets fs.FS) gin.HandlerFunc {
	return func(c *gin.Context) {
		assetPath := strings.TrimPrefix(path.Clean(c.Request.URL.Path), "/")
		data, err := fs.ReadFile(assets, assetPath)
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		contentType := mime.TypeByExtension(path.Ext(assetPath))
		switch path.Ext(assetPath) {
		case ".js", ".mjs":
			contentType = "application/javascript; charset=utf-8"
		case ".css":
			contentType = "text/css; charset=utf-8"
		case ".map", ".json":
			contentType = "application/json; charset=utf-8"
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(http.StatusOK, contentType, data)
	}
}

func Serve(s *server.Server, config *util.Config) {
	gauthConfig := server.GoogleAuthConfig(config.GAuthKeyFile)
	if options.Debug {
		gauthConfig.RedirectURL, _ = url.JoinPath(config.ServerDomain, "/auth/google/callback")
	}
	goth.UseProviders(
		twitter.New(config.TwitterApiKey, config.TwitterApiSecret, config.TwitterApiCallback),
		google.New(
			gauthConfig.ClientID,
			gauthConfig.ClientSecret,
			gauthConfig.RedirectURL,
			"profile", "email", "openid",
		),
	)

	if !options.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	templateFS, err := fs.Sub(assetsFS, "templates")
	if err != nil {
		log.Fatal(err)
	}
	r.HTMLRender = NewFriendRender(templateFS, options.Debug)
	// session
	store := sessions.NewCookieStore([]byte(options.SecretKey))
	r.Use(sessions.Sessions("ffdbsess", store))
	gothic.Store = store

	// Serve static assets
	if options.Debug {
		log.Println("==> debug mode")
		r.Static("/static", "./static")
		r.Static("/app/build/static", "./app/build/static")
	} else {
		r.GET("/static/*path", embeddedAssetHandler(assetsFS))
	}

	// oauth2
	r.GET("/auth/:provider/callback", s.AuthCallback)
	r.GET("/auth/:provider", server.AuthProvider)

	// authed
	authorized := r.Group("/account", server.LoginRequired())
	{
		authorized.GET("/", s.AccountHandler)
		authorized.GET("/import/", s.ImportHandler)
		// authorized.POST("/ffimport/", s.FriendFeedImportHandler)
		authorized.GET("/import/twitter", s.TwitterImportHandler)
		// TODO: fix get
		authorized.GET("/service/:service/delete", s.DeleteServiceHandler)
	}

	r.GET("/", s.HomeHandler)
	r.GET("/favicon.ico", FaviconHandler)
	r.GET("/logout", server.LogoutHandler)

	// TODO: httproute not support "/:name" to catch all
	// see: gin #205
	r.GET("/feed/:name", s.FeedHandler)
	r.GET("/e/:uuid", s.EntryHandler)

	r.GET("/a/entry/:uuid", s.ExpandCommentHandler)
	r.GET("/a/expandlikes/:uuid", s.ExpandLikeHandler)
	action := r.Group("/a", server.LoginRequired())
	{
		action.POST("/share", s.EntryPostHandler)
		action.POST("/upload", s.UploadHandler)
		action.POST("/follow", s.FollowHandler)
		action.POST("/delete", s.EntryDeleteHandler)
		action.POST("/like", s.LikeHandler)
		action.POST("/like/delete", s.LikeDeleteHandler)
		action.POST("/comment", s.CommentHandler)
		action.POST("/comment/delete", s.CommentDeleteHandler)
	}

	r.GET("/public", s.PublicHandler)
	r.GET("/search", s.SearchHandler)
	r.GET("/tag/:name", s.TagHandler)

	r.Static("/file", config.MediaPath)

	r.NoRoute(NotFoundHandler)

	fmt.Println("Starting server...")
	r.Run(fmt.Sprintf(":%v", options.Port))
}

func main() {
	flag.Parse()

	cfg, err := util.NewConfigFromJSON(options.ConfigFile)
	if err != nil {
		log.Fatalf("load config %q: %v", options.ConfigFile, err)
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1024 * 1024 * 64),
			// grpc.MaxCallSendMsgSize(64*1024*1024),
		),
	}
	if cfg.Address == "" {
		cfg.Address = options.Rpc
	}
	rpcConn, err := grpc.Dial(cfg.Address, opts...)
	if err != nil {
		log.Fatalf("Connection error: %v", err)
	}
	defer rpcConn.Close()

	s := server.NewServer(rpcConn, assetsFS, cfg, options.SecretKey, options.Debug)
	go Serve(s, cfg)
	waitShutdown()
}
