package main

import (
	"context"
	"embed"
	"errors"
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
	"time"

	"github.com/flosch/pongo2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	server "github.com/yinhm/friendfeed/httpd/src"
	"github.com/yinhm/friendfeed/pb"
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

const defaultSecretKey = "randombitsreplacedlkjsa"

func init() {
	flag.BoolVar(&options.Debug, "d", false, "Enable debug")
	flag.StringVar(&options.Rpc, "rpc", "localhost:8901", "Rpc Server Address")
	flag.UintVar(&options.Port, "p", 8080, "HTTP server listen port")
	flag.StringVar(&options.SecretKey, "s", defaultSecretKey, "Key used to encryption cookies")
	flag.StringVar(&options.ConfigFile, "c", "/srv/ffdb/config.json", "Config file")

	// babel.Init(runtime.NumCPU())
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

func Serve(s *server.Server, config *util.Config) error {
	gauthConfig, err := server.GoogleAuthConfig(config.GAuthKeyFile)
	if err != nil {
		return err
	}
	if options.Debug {
		gauthConfig.RedirectURL, _ = url.JoinPath(config.ServerDomain, "/auth/google/callback")
		// Allow OAuth callbacks to complete on a different host than the one
		// that started the handshake (LAN IP vs registered localhost
		// callback), where the state cookie cannot follow.
		server.EnableOAuthRelay()
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
		return err
	}
	friendRender, err := NewFriendRender(templateFS, options.Debug)
	if err != nil {
		return err
	}
	r.HTMLRender = friendRender
	// session
	store := cookie.NewStore([]byte(options.SecretKey))
	if options.Debug {
		// gorilla/sessions v1.4 defaults cookies to Secure + SameSite=None,
		// which browsers reject over plain HTTP on non-localhost hosts (e.g.
		// 192.168.x.x); relax the policy so local debug logins stick.
		store.Options(sessions.Options{
			Path:     "/",
			MaxAge:   86400 * 30,
			Secure:   false,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}
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
		authorized.GET("/profile", s.AccountProfileHandler)
		authorized.POST("/profile", s.AccountProfileUpdateHandler)
		authorized.GET("/import/", s.ImportHandler)
		// authorized.POST("/ffimport/", s.FriendFeedImportHandler)
		authorized.GET("/import/twitter", s.TwitterImportHandler)
		authorized.GET("/requests", s.AccountRequestsHandler)
		authorized.POST("/requests/action", s.AccountRequestActionHandler)
		authorized.POST("/feed-service", s.AddFeedServiceHandler)
		authorized.POST("/feed-service/:service/:action", s.FeedServiceActionHandler)
		// TODO: fix get
		authorized.GET("/service/:service/delete", s.DeleteServiceHandler)
	}

	r.GET("/", s.HomeHandler)
	r.GET("/notifications", server.LoginRequired(), s.NotificationsHandler)
	r.GET("/favicon.ico", FaviconHandler)
	r.GET("/logout", server.LogoutHandler)

	// TODO: httproute not support "/:name" to catch all
	// see: gin #205
	r.GET("/feed/:name", s.FeedHandler)
	r.GET("/feed/:name/groups", server.LoginRequired(), s.UserGroupsPageHandler)
	r.GET("/feed/:name/import", server.LoginRequired(), s.FeedImportPageHandler)
	r.GET("/feed/:name/likes", server.LoginRequired(), s.InteractionFeedHandler(pb.InteractionKind_INTERACTION_KIND_LIKE, "likes"))
	r.GET("/feed/:name/comments", server.LoginRequired(), s.InteractionFeedHandler(pb.InteractionKind_INTERACTION_KIND_COMMENT, "comments"))
	r.GET("/feed/:name/following", server.LoginRequired(), s.ProfileRelationsHandler("following"))
	r.GET("/feed/:name/followers", server.LoginRequired(), s.ProfileRelationsHandler("followers"))
	r.GET("/e/:uuid", s.EntryHandler)

	r.GET("/a/entry/:uuid", s.ExpandCommentHandler)
	r.GET("/a/expandlikes/:uuid", s.ExpandLikeHandler)
	action := r.Group("/a", server.LoginRequired())
	{
		action.GET("/events", s.EventsHandler)
		action.POST("/share", s.EntryPostHandler)
		action.POST("/upload", s.UploadHandler)
		action.POST("/upload_file", s.UploadFileHandler)
		action.POST("/follow", s.FollowHandler)
		action.POST("/feed-request", s.FeedRequestHandler)
		action.POST("/feed-request/cancel", s.FeedRequestCancelHandler)
		action.POST("/delete", s.EntryDeleteHandler)
		action.POST("/like", s.LikeHandler)
		action.POST("/like/delete", s.LikeDeleteHandler)
		action.POST("/comment", s.CommentHandler)
		action.POST("/comment/delete", s.CommentDeleteHandler)
	}

	r.GET("/groups", s.GroupDiscoveryPageHandler)
	groups := r.Group("/groups", server.LoginRequired())
	{
		groups.GET("/create", s.GroupCreatePageHandler)
		groups.POST("/create", s.GroupCreateHandler)
		groups.GET("/:name/settings", s.GroupSettingsPageHandler)
		groups.POST("/:name/settings", s.GroupSettingsHandler)
		groups.GET("/:name/members", s.GroupMembersPageHandler)
		groups.POST("/:name/members/action", s.GroupMemberActionHandler)
		groups.POST("/:name/delete", s.GroupDeleteHandler)
	}

	r.GET("/public", s.PublicHandler)
	r.GET("/search", s.SearchHandler)
	r.GET("/tag/:name", s.TagHandler)

	r.GET("/file/*filepath", localMediaHandler(config.MediaPath))
	r.HEAD("/file/*filepath", localMediaHandler(config.MediaPath))

	r.NoRoute(NotFoundHandler)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%v", options.Port),
		Handler: r,
	}
	serverError := make(chan error, 1)
	go func() {
		serverError <- httpServer.ListenAndServe()
	}()

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(shutdownSignal)

	log.Printf("starting webserver on %s", httpServer.Addr)
	select {
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdownSignal:
		log.Println("shutting down webserver...")
	}

	// Long-lived SSE handlers must be signalled before HTTP graceful shutdown;
	// otherwise every deployment can wait for the full 10 second timeout.
	s.ShutdownRealtime()
	s.ShutdownUploadMaintenance()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown webserver: %w", err)
	}
	if err := <-serverError; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func main() {
	flag.Parse()
	if !options.Debug && options.SecretKey == defaultSecretKey {
		log.Fatal("production requires an explicit non-default -s secret")
	}

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
	s.StartRealtime(rpcConn)
	s.StartUploadMaintenance()
	defer s.ShutdownRealtime()
	defer s.ShutdownUploadMaintenance()
	if err := Serve(s, cfg); err != nil {
		log.Fatalf("webserver: %v", err)
	}
}
