// gin handlers to provide user login via an OAuth 2.0 backend.
package server

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/markbates/goth/gothic"
	"github.com/patrickmn/go-cache"
	"github.com/yinhm/friendfeed/pb"
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
			c.Abort()
			return
		}
		if CurrentUserUuid(c) == "" {
			LogoutHandler(c)
			c.Abort()
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
	if err != nil || n.IsAbs() || n.Host != "" {
		return "/"
	}
	path := n.Path
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "/"
	}
	if n.RawQuery != "" {
		path += "?" + n.RawQuery
	}
	return path
}

func oauthNextKey(provider string) string {
	return "oauth_next_" + provider
}

func setGothProvider(req *http.Request, provider string) {
	query := req.URL.Query()
	query.Set("provider", provider)
	req.URL.RawQuery = query.Encode()
}

func AuthProvider(c *gin.Context) {
	provider := c.Params.ByName("provider")
	sess := sessions.Default(c)
	sess.Set(oauthNextKey(provider), extractNextPath(c.Request.URL.Query().Get("next")))
	if err := sess.Save(); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	setGothProvider(c.Request, provider)
	for attempt := 1; attempt <= 2; attempt++ {
		authURL, err := gothic.GetAuthURL(c.Writer, c.Request)
		if err == nil {
			http.Redirect(c.Writer, c.Request, authURL, http.StatusTemporaryRedirect)
			return
		}
		if attempt == 2 {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		log.Printf("OAuth request-token attempt failed for %s; retrying once: %v", provider, err)
	}
}

func (s *Server) AuthCallback(c *gin.Context) {
	setGothProvider(c.Request, c.Params.ByName("provider"))
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
		Name:              u.NickName, // gothic use screen_name as nickname
		NickName:          u.Name,
		Email:             u.Email,
		AccessToken:       u.AccessToken,
		AccessTokenSecret: u.AccessTokenSecret,
		Provider:          provider,
		AvatarUrl:         u.AvatarURL,
		Description:       u.Description,
	}

	// FIXME: WTF
	// user should not been logged yet
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

	// Old behavior allow google only
	// Now allow twitter login aswell
	sess := sessions.Default(c)
	sess.Set("user_id", u.UserID)
	sess.Set("uuid", profile.Uuid)
	next, _ := sess.Get(oauthNextKey(provider)).(string)
	sess.Delete(oauthNextKey(provider))
	if err := sess.Save(); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	next = extractNextPath(next)
	http.Redirect(c.Writer, c.Request, next, http.StatusFound)
}

func GoogleAuthConfig(keyPath string) *oauth2.Config {
	jsonKey, err := os.ReadFile(keyPath)
	if err != nil {
		log.Fatal(err)
	}
	conf, _ := google.ConfigFromJSON(jsonKey, "profile")
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
	v, found := s.cache.Get(cacheKey)
	if !found {
		req := &pb.ProfileRequest{Uuid: uuid}
		graph, err := s.client.FetchGraph(ctx, req)
		if err != nil {
			return nil, err
		}
		s.cache.Set(cacheKey, graph, cache.DefaultExpiration)
		return graph, nil
	}
	return v.(*pb.Graph), nil
}

func (s *Server) feedWritable(c *gin.Context, feedUuid string) bool {
	// owner feed
	user, err := s.CurrentUser(c)
	if err != nil {
		return false
	}
	if user.Uuid == "" {
		return false
	}
	if user.Uuid == feedUuid {
		return true
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	// group feed
	feedProfile, err := s.client.FetchProfile(ctx, &pb.ProfileRequest{Uuid: feedUuid})
	if err != nil || feedProfile == nil {
		return false
	}
	if feedProfile.Type != "group" {
		return false
	}

	fReq := &pb.FollowRequest{
		ProfileUuid: user.Uuid,
		FeedUuid:    feedUuid,
		Action:      "isFollow",
	}
	fResp, err := s.client.GraphFollow(ctx, fReq)
	if err != nil {
		return false
	}
	return fResp.Followed
}
