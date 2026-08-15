package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
)

type relayFakeSession struct {
	authorized bool
}

func (s *relayFakeSession) GetAuthURL() (string, error) {
	return "https://provider.example/auth?state=state-123", nil
}
func (s *relayFakeSession) Marshal() string { return "marshaled-session" }
func (s *relayFakeSession) Authorize(_ goth.Provider, params goth.Params) (string, error) {
	if params.Get("state") != "state-123" {
		return "", errors.New("state mismatch")
	}
	s.authorized = true
	return "access-token", nil
}

type relayFakeProvider struct {
	session *relayFakeSession
}

func (p *relayFakeProvider) Name() string                               { return "relayfake" }
func (p *relayFakeProvider) SetName(string)                             {}
func (p *relayFakeProvider) Debug(bool)                                 {}
func (p *relayFakeProvider) RefreshToken(string) (*oauth2.Token, error) { return nil, nil }
func (p *relayFakeProvider) RefreshTokenAvailable() bool                { return false }
func (p *relayFakeProvider) BeginAuth(string) (goth.Session, error)     { return p.session, nil }
func (p *relayFakeProvider) UnmarshalSession(raw string) (goth.Session, error) {
	if raw != "marshaled-session" {
		return nil, errors.New("bad session")
	}
	return p.session, nil
}
func (p *relayFakeProvider) FetchUser(goth.Session) (goth.User, error) {
	if !p.session.authorized {
		return goth.User{}, errors.New("not authorized")
	}
	return goth.User{UserID: "user-1"}, nil
}

func TestOauthHandshakeID(t *testing.T) {
	require.Equal(t, "abc", oauthHandshakeID("https://p.example/auth?client_id=x&state=abc"))
	require.Equal(t, "tok", oauthHandshakeID("https://p.example/auth?oauth_token=tok"))
	require.Equal(t, "", oauthHandshakeID("https://p.example/auth?client_id=x"))
	require.Equal(t, "", oauthHandshakeID("://not-a-url"))
}

func TestOAuthRelayCompletesAcrossHosts(t *testing.T) {
	EnableOAuthRelay()
	defer func() { oauthRelay = nil }()
	gothic.Store = sessions.NewCookieStore([]byte("relay-test-secret"))

	provider := &relayFakeProvider{session: &relayFakeSession{}}
	goth.UseProviders(provider)

	// The handshake starts on the LAN IP host.
	start := httptest.NewRequest("GET", "http://192.168.1.2/auth/relayfake", nil)
	recorder := httptest.NewRecorder()
	authURL, err := authURLForProvider(recorder, start, "relayfake")
	require.NoError(t, err)
	require.Contains(t, authURL, "state=state-123")

	// The callback arrives on whatever host the URL was rewritten to; no
	// state cookie is involved.
	callback := httptest.NewRequest("GET", "http://localhost/auth/relayfake/callback?state=state-123&code=x", nil)
	user, err := completeUserAuthViaRelay("relayfake", callback)
	require.NoError(t, err)
	require.Equal(t, "user-1", user.UserID)
	require.True(t, provider.session.authorized)

	// Stashed sessions are single-use.
	_, err = completeUserAuthViaRelay("relayfake", callback)
	require.Error(t, err)
}

func TestOAuthRelayRejectsUnknownHandshake(t *testing.T) {
	EnableOAuthRelay()
	defer func() { oauthRelay = nil }()

	callback := httptest.NewRequest("GET", "http://192.168.1.2/auth/relayfake/callback?state=nope", nil)
	_, err := completeUserAuthViaRelay("relayfake", callback)
	require.Error(t, err)

	noState := httptest.NewRequest("GET", "http://192.168.1.2/auth/relayfake/callback", nil)
	_, err = completeUserAuthViaRelay("relayfake", noState)
	require.Error(t, err)
}

func TestOAuthRelayDisabledByDefault(t *testing.T) {
	callback := httptest.NewRequest("GET", "http://192.168.1.2/auth/google/callback?state=x", nil)
	_, err := completeUserAuthViaRelay("google", callback)
	require.Error(t, err)
}

// 跨 host 回调的完整链路：握手在带 cookie 的会话里开始，回调完全不携带任何
// cookie（模拟浏览器把回调 host 改写为另一个地址），relay 必须完成登录并
// 发出可用的 ffdbsess 会话 cookie。
func TestAuthCallbackViaRelayWithoutCookies(t *testing.T) {
	var gotAuth *pb.OAuthUser
	client := &fakeApiClient{
		putOAuth: func(ctx context.Context, in *pb.OAuthUser, opts ...grpc.CallOption) (*pb.Profile, error) {
			gotAuth = in
			return &pb.Profile{Uuid: "uuid-123"}, nil
		},
	}
	router := newFauxAuthRouter(t, client)
	EnableOAuthRelay()
	t.Cleanup(func() { oauthRelay = nil })

	// 握手开始，cookie 全部丢弃——回调落在另一个 host 时浏览器不会带它们。
	_, state := beginFauxAuth(t, router, "/feed/x")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/auth/faux/callback?state="+state, nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("callback status = %d; want %d (body: %s)", recorder.Code, http.StatusFound, recorder.Body.String())
	}
	if gotAuth == nil {
		t.Fatal("PutOAuth was not called")
	}
	if gotAuth.UserId != "id" {
		t.Errorf("UserId = %q; want %q", gotAuth.UserId, "id")
	}

	// 回调签发的会话 cookie 必须能证明登录态。
	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("callback did not set a session cookie")
	}
	whoRecorder := httptest.NewRecorder()
	router.ServeHTTP(whoRecorder, callbackRequest("/whoami", cookies))
	if body := whoRecorder.Body.String(); body != "id|uuid-123" {
		t.Fatalf("whoami = %q; want %q", body, "id|uuid-123")
	}
}
