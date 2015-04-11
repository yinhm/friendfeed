// gin handlers to provide user login via an OAuth 2.0 backend.
package server

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"

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
			next := url.QueryEscape(c.Request.URL.RequestURI())
			http.Redirect(c.Writer, c.Request, loginUrl+"?next="+next, http.StatusFound)
		}
	}
}

func LogoutHandler(c *gin.Context) {
	sess := sessions.Default(c)
	next := extractNextPath(c.Request.URL.Query().Get("next"))
	sess.Delete("provider")
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

	// print our state string to the console
	fmt.Println("get state callback", gothic.GetState(c.Request))

	provider, _ := gothic.GetProviderName(c.Request)
	u, err := gothic.CompleteUserAuth(c.Writer, c.Request)
	if err != nil {
		c.String(400, err.Error())
		return
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	protoU := &pb.OAuthUser{
		UserId:      u.UserID,
		Name:        u.Name,
		NickName:    u.NickName,
		Email:       u.Email,
		AccessToken: u.AccessToken,
		Provider:    provider,
	}

	profile, err := s.client.Auth(ctx, protoU)
	if RequestError(c, err) {
		return
	}

	sess := sessions.Default(c)
	sess.Set("user_id", u.UserID)
	sess.Set("provider", provider)
	if profile.Uuid != "" {
		sess.Set("uuid", profile.Uuid)
	} else {
		sess.Set("uuid", "")
	}
	sess.Save()

	next := extractNextPath(c.Request.URL.Query().Get("state"))
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
