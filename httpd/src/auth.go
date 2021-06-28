// gin handlers to provide user login via an OAuth 2.0 backend.
package server

import (
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/markbates/goth/gothic"
	pb "github.com/yinhm/friendfeed/proto"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func LoginRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		loginUrl := "/auth/google" // TODO: login page for all privider
		sess := sessions.Default(c)
		if sess.Get("user_id") == nil || sess.Get("user_id").(string) == "" {
			if c.Request.Header.Get("X-Requested-With") == "XMLHttpRequest" {
				c.AbortWithStatus(401)
				return
			}
			next := url.QueryEscape(c.Request.URL.RequestURI())
			http.Redirect(c.Writer, c.Request, loginUrl+"?next="+next, http.StatusFound)
		}
	}
}

func LogoutHandler(c *gin.Context) {
	sess := sessions.Default(c)
	next := extractNextPath(c.Request.URL.Query().Get("next"))
	sess.Delete("user_id")
	sess.Delete("uuid")
	sess.Save()
	http.Redirect(c.Writer, c.Request, next, http.StatusFound)
}

func extractNextPath(next string) string {
	n, err := url.Parse(next)
	if err != nil {
		return "/"
	}
	path := n.Path
	if path == "" {
		path = "/"
	}
	return path
}

func AuthProvider(c *gin.Context) {
	fn := gothic.GetProviderName
	gothic.GetProviderName = func(req *http.Request) (string, error) {
		provider := c.Params.ByName("provider")
		if provider == "" {
			return fn(req)
		}
		return provider, nil
	}
	gothic.BeginAuthHandler(c.Writer, c.Request)
}

func (s *Server) AuthCallback(c *gin.Context) {
	fn := gothic.GetProviderName
	gothic.GetProviderName = func(req *http.Request) (string, error) {
		provider := c.Params.ByName("provider")
		if provider == "" {
			return fn(req)
		}
		return provider, nil
	}

	provider, _ := gothic.GetProviderName(c.Request)
	u, err := gothic.CompleteUserAuth(c.Writer, c.Request)
	if err != nil {
		c.String(400, err.Error())
		return
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	authinfo := &pb.OAuthUser{
		UserId:            u.UserID,
		Name:              u.Name,
		NickName:          u.NickName,
		Email:             u.Email,
		AccessToken:       u.AccessToken,
		AccessTokenSecret: u.AccessTokenSecret,
		Provider:          provider,
	}

	profile, err := s.CurrentUser(c)
	if err != nil {
		c.AbortWithError(500, err)
		return
	}
	authinfo.Uuid = profile.Uuid
	profile, err = s.client.PutOAuth(ctx, authinfo)
	if RequestError(c, err) {
		return
	}

	// Only allow login from google
	// Twitter only for importing feed
	if provider == "google" {
		sess := sessions.Default(c)
		sess.Set("user_id", u.UserID)
		sess.Set("uuid", profile.Uuid)
		sess.Save()
	}

	next := extractNextPath(c.Request.URL.Query().Get("state"))
	if next == "/" && provider == "twitter" {
		next = "/account/import"
	}
	http.Redirect(c.Writer, c.Request, next, http.StatusFound)
}

func GoogleAuthConfig(keyPath string, debug bool) *oauth2.Config {
	jsonKey, err := ioutil.ReadFile(keyPath)
	if err != nil {
		log.Fatal(err)
	}
	conf, _ := google.ConfigFromJSON(jsonKey, "profile")
	if debug {
		conf.RedirectURL = "http://localhost:8080/auth/google/callback"
	}
	return conf
}

func CurrentUserId(c *gin.Context) string {
	sess := sessions.Default(c)
	if sess.Get("user_id") == nil {
		return ""
	}
	return sess.Get("user_id").(string)
}

func CurrentUserUuid(c *gin.Context) string {
	sess := sessions.Default(c)
	if sess.Get("uuid") == nil {
		return ""
	}
	return sess.Get("uuid").(string)
}

func (s *Server) GraphFrom(uuid string) (*pb.Graph, error) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	graph := new(pb.Graph)
	if uuid == "" {
		return graph, nil
	}

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
	return graph, nil
}

func (s *Server) feedWritable(c *gin.Context, feedId string) bool {
	// owner feed
	user, err := s.CurrentUser(c)
	if err != nil {
		return false
	}
	if user.Id == feedId {
		return true
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	// group feed
	profile, err := s.client.FetchProfile(ctx, &pb.ProfileRequest{Uuid: feedId})
	if err != nil || profile == nil {
		return false
	}
	if profile.Type != "group" {
		return false
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
