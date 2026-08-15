package server

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/patrickmn/go-cache"
)

// The OAuth handshake state normally lives in the "_gothic_session" cookie,
// which is scoped to one host. Local testing often browses the dev server by
// LAN IP while providers only accept the registered localhost callback, so
// the callback URL's host gets rewritten by hand and the state cookie never
// reaches the callback — gothic then fails instantly with "could not find a
// matching session for this request".
//
// In debug mode the marshaled goth session is additionally stashed
// server-side, keyed by the handshake id (OAuth2 state / OAuth1 request
// token), letting the callback complete on any host. Entries are single-use
// and expire quickly; production never enables this.
var oauthRelay *cache.Cache

// EnableOAuthRelay turns on host-agnostic OAuth callbacks. Debug mode only.
func EnableOAuthRelay() {
	oauthRelay = cache.New(10*time.Minute, time.Minute)
}

// authURLForProvider starts the provider handshake like gothic.GetAuthURL,
// but with the relay enabled it also stashes the marshaled session so the
// callback can complete without the state cookie.
func authURLForProvider(res http.ResponseWriter, req *http.Request, providerName string) (string, error) {
	if oauthRelay == nil {
		return gothic.GetAuthURL(res, req)
	}
	provider, err := goth.GetProvider(providerName)
	if err != nil {
		return "", err
	}
	sess, err := provider.BeginAuth(gothic.SetState(req))
	if err != nil {
		return "", err
	}
	authURL, err := sess.GetAuthURL()
	if err != nil {
		return "", err
	}
	marshaled := sess.Marshal()
	if err := gothic.StoreInSession(providerName, marshaled, req, res); err != nil {
		return "", err
	}
	if key := oauthHandshakeID(authURL); key != "" {
		oauthRelay.Set(providerName+":"+key, marshaled, cache.DefaultExpiration)
	}
	return authURL, nil
}

// completeUserAuthViaRelay finishes the provider handshake from the stashed
// session instead of the host-scoped state cookie. The stash key is the
// handshake id from the callback itself, so a lookup hit binds the callback
// to the original auth request.
func completeUserAuthViaRelay(providerName string, req *http.Request) (goth.User, error) {
	if oauthRelay == nil {
		return goth.User{}, errors.New("oauth relay is not enabled")
	}
	key := req.URL.Query().Get("state")
	if key == "" {
		key = req.URL.Query().Get("oauth_token")
	}
	if key == "" {
		return goth.User{}, errors.New("oauth callback carries neither state nor oauth_token")
	}
	stashKey := providerName + ":" + key
	raw, found := oauthRelay.Get(stashKey)
	if !found {
		return goth.User{}, errors.New("no stashed oauth session for this callback")
	}
	oauthRelay.Delete(stashKey) // single use

	provider, err := goth.GetProvider(providerName)
	if err != nil {
		return goth.User{}, err
	}
	sess, err := provider.UnmarshalSession(raw.(string))
	if err != nil {
		return goth.User{}, err
	}
	if _, err := sess.Authorize(provider, req.URL.Query()); err != nil {
		return goth.User{}, err
	}
	return provider.FetchUser(sess)
}

// oauthHandshakeID extracts the value that also appears on the provider's
// callback: the OAuth2 state, or the OAuth1 request token.
func oauthHandshakeID(authURL string) string {
	u, err := url.Parse(authURL)
	if err != nil {
		return ""
	}
	if state := u.Query().Get("state"); state != "" {
		return state
	}
	return u.Query().Get("oauth_token")
}
