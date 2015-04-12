package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
	flags "github.com/jessevdk/go-flags"
	server "github.com/yinhm/friendfeed/httpd/src"
	"google.golang.org/grpc"

	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/gplus"
)

var options struct {
	Debug bool `short:"d" description:"Enable debug" default:"false" env:"DEBUG"`
	// RpcAddress string `short:"rpc" description:"Rpc Server Address" default:"localhost:8901" env:"RPC_ADDRESS"`
	Port      uint   `short:"p" description:"HTTP server listen port" default:"8080" env:"PORT"`
	SecretKey string `short:"s" description:"Key used to encryption cookies" default:"randombitsreplacedlkjsa" env:"SECRET_KEY"`
	KeyFile   string `short:"f" description:"Google OAuth2 Client Config" env:"GoogleKeyFile"`
}

func init() {
	_, err := flags.ParseArgs(&options, os.Args)

	if err != nil {
		os.Exit(1)
	}
}

func waitShutdown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)

	select {
	case _ = <-c:
		log.Println("Waiting for shutdown...")
		return
	}
}

func assetContentType(name string) string {
	ext := filepath.Ext(name)
	return mime.TypeByExtension(ext)
}

func serveAsset(path string, c *gin.Context) {
	buff, err := Asset(path)

	if err != nil {
		c.String(400, err.Error())
		return
	}

	c.Data(200, assetContentType(path), buff)
}

func AssetHandler(c *gin.Context) {
	path := "static" + c.Params.ByName("path")
	serveAsset(path, c)
}

func NotFoundHandler(c *gin.Context) {
	ctx := pongo2.Context{
		"title": "Frienfeed",
		"name":  "404 not found",
	}
	c.HTML(404, "404.html", ctx)
}

func FaviconHandler(c *gin.Context) {
	favStr := "AAABAAEAEBAAAAAAAABoBQAAFgAAACgAAAAQAAAAIAAAAAEACAAAAAAAQAEAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAA////AMd+QwDov50A2J9xAOSvhADQjVcA78ywANWVYQDMhk0A5biUAOzHqQDepHUA\n57OJANmcawDJgkgA0JBdAOKrfwDrupMA6sKiAM6JUgDXmGYAzoxXAMiBRwDRkV4AAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAgICAgICAgICAgICAgICAgICBhISEQIYAhESEgYCCgICAgYSEhECGAIREhIGAgoCAgIGEhIR\nAgICERISBgICAgIGFRISBQYGBgUSEhUGDwICEhISEhISEhISEhISEgYCAhISEhISEhISEhISEhIG\nAgIGFRISBQYGBgUSEhUGDwICAgYSEg0PDwIMEhIMAgkCAgIJEhISEhIUDhISEhIFAgIQAggSEhIS\nCA8FEhISEgICAxcCDwYGCQICAgkGBg8CAgcDEAICAgIQBBYCAgICAgIHBwcLCgoTBwcHEwoKCwIC\nBwcHBwcHBwcHBwcHBwcCAgICAgICAgICAgICAgICAgAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	favicon, _ := base64.StdEncoding.DecodeString(favStr)
	flen := strconv.Itoa(len(favicon))

	c.Writer.Header().Set("Content-Type", "image/png")
	c.Writer.Header().Set("Content-Length", flen)
	c.Writer.Write(favicon)
}

func Serve(s *server.Server) {
	gauthConfig := server.GoogleAuthConfig(options.KeyFile, options.Debug)
	goth.UseProviders(
		// twitter.New("yDY5kMcl71OAdHDiJc0S7MMOk", "vWsANrwaNHeIsdKg0t5AEixgUgbQZiQGpoklDfHhSnpBKQCMlP", "https://friendfeed.me/auth/twitter/callback"),
		gplus.New(gauthConfig.ClientID, gauthConfig.ClientSecret, gauthConfig.RedirectURL),
	)
	providers := goth.GetProviders()
	providers["google"] = providers["gplus"]

	// Assign the GetState function variable so we can return the
	// state string we want to get back at the end of the oauth process.
	// Only works with facebook and gplus providers.
	gothic.GetState = func(req *http.Request) string {
		// Get the state string from the query parameters.
		return req.URL.Query().Get("next")
	}

	r := gin.Default()
	r.HTMLRender = NewRender()
	// session
	store := sessions.NewCookieStore([]byte(options.SecretKey))
	r.Use(sessions.Sessions("ffsession", store))

	// Serve static assets
	if options.Debug {
		log.Println("==> debug mode")
		r.Static("/static", "./static")
	} else {
		r.GET("/static/*path", AssetHandler)
	}

	// oauth2
	r.GET("/auth/:provider/callback", s.AuthCallback)
	r.GET("/auth/:provider", server.AuthProvider)

	// authed
	authorized := r.Group("/account", server.LoginRequired())
	{
		authorized.GET("/", s.AccountHandler)
		authorized.GET("/import/", s.ImportHandler)
		authorized.POST("/ffimport/", s.FriendFeedImportHandler)
	}

	r.GET("/", s.HomeHandler)
	r.GET("favicon.ico", FaviconHandler)
	r.GET("/logout", server.LogoutHandler)

	// TODO: httproute not support "/:name" to catch all
	// see: gin #205
	r.GET("/feed/:name", s.FeedHandler)
	r.GET("/e/:uuid", s.EntryHandler)

	r.GET("/a/entry/:uuid", s.EntryCommentHandler)
	action := r.Group("/a", server.LoginRequired())
	{
		action.POST("/like", s.LikeHandler)
		action.POST("/like/delete", s.LikeDeleteHandler)
		action.POST("/comment", s.CommentHandler)
	}

	r.NotFound404(NotFoundHandler)

	fmt.Println("Starting server...")
	r.Run(fmt.Sprintf(":%v", options.Port))
}

func main() {
	rpcConn, err := grpc.Dial("localhost:8901")
	if err != nil {
		log.Fatalf("Connection error: %v", err)
	}
	defer rpcConn.Close()

	if !options.Debug {
		dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			log.Fatal(err)
		}
		path := filepath.Join(dir, "templates")
		log.Printf("==> chdir to: %s", path)
		if err = os.Chdir(path); err != nil {
			panic("chdir failed")
		}
	}

	s := server.NewServer(rpcConn, options.SecretKey)
	go Serve(s)
	waitShutdown()
}
