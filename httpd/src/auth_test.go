package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/faux"
	gsessions "github.com/gorilla/sessions"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
)

func TestExtractNextPath(t *testing.T) {
	tests := map[string]string{
		"":                                  "/",
		"/":                                 "/",
		"/feed/yinhm?page=2":                "/feed/yinhm?page=2",
		"hw-y778egVoO5g6pkV4z4tYZhSvivZkig": "/",
		"https://evil.example/auth/google":  "/",
		"//evil.example/auth/google":        "/",
	}
	for input, want := range tests {
		if got := extractNextPath(input); got != want {
			t.Errorf("extractNextPath(%q) = %q; want %q", input, got, want)
		}
	}
}

func TestSetGothProvider(t *testing.T) {
	for _, want := range []string{"google", "twitter"} {
		req := httptest.NewRequest(http.MethodGet, "/auth/"+want+"/callback", nil)
		setGothProvider(req, want)

		got, err := gothic.GetProviderName(req)
		if err != nil {
			t.Fatalf("GetProviderName(%q): %v", want, err)
		}
		if got != want {
			t.Fatalf("GetProviderName(%q) = %q; want %q", want, got, want)
		}
	}
}

func TestShowShareForUser(t *testing.T) {
	tests := []struct {
		name      string
		userUuid  string
		requested bool
		want      bool
	}{
		{name: "anonymous", requested: true, want: false},
		{name: "authenticated and writable", userUuid: "user-uuid", requested: true, want: true},
		{name: "authenticated but read-only", userUuid: "user-uuid", requested: false, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := showShareForUser(test.userUuid, test.requested); got != test.want {
				t.Fatalf("showShareForUser(%q, %t) = %t; want %t", test.userUuid, test.requested, got, test.want)
			}
		})
	}
}

func TestLoginRequiredStopsAnonymousRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		xhr          bool
		wantStatus   int
		wantLocation string
	}{
		{
			name:         "browser request redirects to login",
			wantStatus:   http.StatusFound,
			wantLocation: "/auth/google?next=%2Fprivate%3Ftab%3D1",
		},
		{
			name:       "ajax request returns unauthorized",
			xhr:        true,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handlerRan := false
			router := gin.New()
			router.Use(sessions.Sessions("test", cookie.NewStore([]byte("test-secret"))))
			router.GET("/private", LoginRequired(), func(c *gin.Context) {
				handlerRan = true
				c.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodGet, "/private?tab=1", nil)
			if test.xhr {
				request.Header.Set("X-Requested-With", "XMLHttpRequest")
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			if handlerRan {
				t.Fatal("protected handler ran for an anonymous request")
			}
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d; want %d", recorder.Code, test.wantStatus)
			}
			if location := recorder.Header().Get("Location"); location != test.wantLocation {
				t.Fatalf("Location = %q; want %q", location, test.wantLocation)
			}
		})
	}
}

func TestSessionCookieRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(sessions.Sessions("ffdbsess", cookie.NewStore([]byte("test-secret"))))
	router.GET("/login", func(c *gin.Context) {
		sess := sessions.Default(c)
		sess.Set("user_id", "user-1")
		sess.Set("uuid", "uuid-1")
		if err := sess.Save(); err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	handlerRan := false
	router.GET("/private", LoginRequired(), func(c *gin.Context) {
		handlerRan = true
		if got := CurrentUserId(c); got != "user-1" {
			t.Errorf("CurrentUserId = %q; want %q", got, "user-1")
		}
		if got := CurrentUserUuid(c); got != "uuid-1" {
			t.Errorf("CurrentUserUuid = %q; want %q", got, "uuid-1")
		}
		c.Status(http.StatusNoContent)
	})

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	cookies := loginRecorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set a session cookie")
	}

	request := httptest.NewRequest(http.MethodGet, "/private", nil)
	for _, ck := range cookies {
		request.AddCookie(ck)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if !handlerRan {
		t.Fatal("protected handler did not run for an authenticated request")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d; want %d", recorder.Code, http.StatusNoContent)
	}
}

// gorilla/sessions 是迁移前后（gin-gonic/contrib 与 gin-contrib）共同的底层
// 编码器。用 gorilla 原生 CookieStore 签发的 cookie 必须能被新中间件解码，
// 以此锁定 session cookie 的持久化格式，保证迁移后旧 cookie 仍然有效。
func TestSessionCookieGorillaCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gstore := gsessions.NewCookieStore([]byte("test-secret"))
	issueReq := httptest.NewRequest(http.MethodGet, "/", nil)
	issueRecorder := httptest.NewRecorder()
	gsess, err := gstore.Get(issueReq, "ffdbsess")
	if err != nil {
		t.Fatalf("gorilla Get: %v", err)
	}
	gsess.Values["user_id"] = "user-1"
	if err := gsess.Save(issueReq, issueRecorder); err != nil {
		t.Fatalf("gorilla Save: %v", err)
	}
	cookies := issueRecorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("gorilla store did not issue a session cookie")
	}

	router := gin.New()
	router.Use(sessions.Sessions("ffdbsess", cookie.NewStore([]byte("test-secret"))))
	router.GET("/whoami", func(c *gin.Context) {
		c.String(http.StatusOK, CurrentUserId(c))
	})

	request := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	for _, ck := range cookies {
		request.AddCookie(ck)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", recorder.Code, http.StatusOK)
	}
	if body := recorder.Body.String(); body != "user-1" {
		t.Fatalf("whoami = %q; want %q", body, "user-1")
	}
}

// fakeApiClient 嵌入 pb.ApiClient 接口，只覆盖测试用到的方法；
// 未覆盖的方法被调用时会 panic，恰好能暴露意外的 RPC 调用。
type fakeApiClient struct {
	pb.ApiClient
	putOAuth func(ctx context.Context, in *pb.OAuthUser, opts ...grpc.CallOption) (*pb.Profile, error)
}

func (f *fakeApiClient) PutOAuth(ctx context.Context, in *pb.OAuthUser, opts ...grpc.CallOption) (*pb.Profile, error) {
	return f.putOAuth(ctx, in, opts...)
}

// newFauxAuthRouter 注册 goth 的 faux 测试 provider，搭建与 main.go 一致的
// /auth/:provider 与 /auth/:provider/callback 路由，外加一个回显会话身份的
// /whoami 探针。
//
// 注意：goth 的 provider registry 和 gothic.Store 都是进程级全局状态，
// 这里保存并在 Cleanup 中恢复测试前的值；因此这些测试不能 t.Parallel()。
func newFauxAuthRouter(t *testing.T, client pb.ApiClient) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	savedProviders := goth.GetProviders()
	goth.ClearProviders()
	goth.UseProviders(&faux.Provider{})
	t.Cleanup(func() {
		goth.ClearProviders()
		restored := make([]goth.Provider, 0, len(savedProviders))
		for _, p := range savedProviders {
			restored = append(restored, p)
		}
		goth.UseProviders(restored...)
	})

	oldStore := gothic.Store
	gothic.Store = gsessions.NewCookieStore([]byte("test-secret"))
	t.Cleanup(func() { gothic.Store = oldStore })

	s := &Server{client: client}
	router := gin.New()
	router.Use(sessions.Sessions("ffdbsess", cookie.NewStore([]byte("test-secret"))))
	router.GET("/auth/:provider", AuthProvider)
	router.GET("/auth/:provider/callback", s.AuthCallback)
	router.GET("/whoami", func(c *gin.Context) {
		c.String(http.StatusOK, CurrentUserId(c)+"|"+CurrentUserUuid(c))
	})
	return router
}

// beginFauxAuth 走一遍 /auth/faux，返回 gin+gothic 的会话 cookie 和授权 URL
// 中的 state，用于随后模拟 provider 回调。
func beginFauxAuth(t *testing.T, router *gin.Engine, next string) ([]*http.Cookie, string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/auth/faux?next="+url.QueryEscape(next), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusTemporaryRedirect {
		t.Fatalf("auth start status = %d; want %d", recorder.Code, http.StatusTemporaryRedirect)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect %q: %v", recorder.Header().Get("Location"), err)
	}
	state := location.Query().Get("state")
	if state == "" {
		t.Fatalf("auth url %q has no state", location)
	}
	return recorder.Result().Cookies(), state
}

func callbackRequest(target string, cookies []*http.Cookie) *http.Request {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for _, ck := range cookies {
		request.AddCookie(ck)
	}
	return request
}

// 完整回调流程：授权跳转 -> 回调换码 -> PutOAuth -> 写 session -> 跳回 next。
func TestAuthCallbackSuccess(t *testing.T) {
	var gotAuth *pb.OAuthUser
	client := &fakeApiClient{
		putOAuth: func(ctx context.Context, in *pb.OAuthUser, opts ...grpc.CallOption) (*pb.Profile, error) {
			gotAuth = in
			return &pb.Profile{Uuid: "uuid-123"}, nil
		},
	}
	router := newFauxAuthRouter(t, client)

	cookies, state := beginFauxAuth(t, router, "/feed/x")

	// faux.BeginAuth 只填 ID 和 AuthURL。为了让名称字段映射的断言有意义，
	// 用同一会话 cookie 覆盖写入一个带显示名和邮箱的 session（state 不变）。
	enriched := &faux.Session{
		ID:      "id",
		Name:    "Faux User",
		Email:   "faux@example.com",
		AuthURL: "http://example.com/auth?state=" + url.QueryEscape(state),
	}
	storeReq := httptest.NewRequest(http.MethodGet, "/", nil)
	storeRec := httptest.NewRecorder()
	if err := gothic.StoreInSession("faux", enriched.Marshal(), storeReq, storeRec); err != nil {
		t.Fatalf("StoreInSession: %v", err)
	}
	// 替换（而非追加）gothic 会话 cookie，否则旧值会遮蔽新值
	kept := cookies[:0]
	for _, ck := range cookies {
		if ck.Name != gothic.SessionName {
			kept = append(kept, ck)
		}
	}
	cookies = append(kept, storeRec.Result().Cookies()...)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, callbackRequest("/auth/faux/callback?state="+state, cookies))

	if recorder.Code != http.StatusFound {
		t.Fatalf("callback status = %d; want %d", recorder.Code, http.StatusFound)
	}
	if location := recorder.Header().Get("Location"); location != "/feed/x" {
		t.Fatalf("Location = %q; want %q", location, "/feed/x")
	}

	if gotAuth == nil {
		t.Fatal("PutOAuth was not called")
	}
	if gotAuth.Provider != "faux" {
		t.Errorf("Provider = %q; want %q", gotAuth.Provider, "faux")
	}
	if gotAuth.UserId != "id" {
		t.Errorf("UserId = %q; want %q", gotAuth.UserId, "id")
	}
	if gotAuth.AccessToken != "access" {
		t.Errorf("AccessToken = %q; want %q", gotAuth.AccessToken, "access")
	}
	// 名称映射契约：authinfo.NickName 必须是显示名（u.Name），
	// authinfo.Name 必须是 screen_name（u.NickName，faux 不设置，故为空）。
	// 两者写反时下面的断言会失败。
	if gotAuth.NickName != "Faux User" {
		t.Errorf("NickName = %q; want display name %q", gotAuth.NickName, "Faux User")
	}
	if gotAuth.Name != "" {
		t.Errorf("Name = %q; want screen name (faux leaves it empty)", gotAuth.Name)
	}
	if gotAuth.Email != "faux@example.com" {
		t.Errorf("Email = %q; want %q", gotAuth.Email, "faux@example.com")
	}

	// 回调写入了会话身份
	request := callbackRequest("/whoami", recorder.Result().Cookies())
	whoRecorder := httptest.NewRecorder()
	router.ServeHTTP(whoRecorder, request)
	if body := whoRecorder.Body.String(); body != "id|uuid-123" {
		t.Fatalf("whoami = %q; want %q", body, "id|uuid-123")
	}
}

func TestAuthCallbackStateMismatch(t *testing.T) {
	called := false
	client := &fakeApiClient{
		putOAuth: func(ctx context.Context, in *pb.OAuthUser, opts ...grpc.CallOption) (*pb.Profile, error) {
			called = true
			return &pb.Profile{}, nil
		},
	}
	router := newFauxAuthRouter(t, client)

	cookies, _ := beginFauxAuth(t, router, "/")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, callbackRequest("/auth/faux/callback?state=wrong-state", cookies))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("callback status = %d; want %d", recorder.Code, http.StatusBadRequest)
	}
	if called {
		t.Fatal("PutOAuth must not be called on state mismatch")
	}
}

func TestAuthCallbackWithoutSession(t *testing.T) {
	router := newFauxAuthRouter(t, &fakeApiClient{})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/auth/faux/callback?state=x", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("callback status = %d; want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestAuthCallbackPutOAuthFailure(t *testing.T) {
	client := &fakeApiClient{
		putOAuth: func(ctx context.Context, in *pb.OAuthUser, opts ...grpc.CallOption) (*pb.Profile, error) {
			return nil, errors.New("boom")
		},
	}
	router := newFauxAuthRouter(t, client)

	cookies, state := beginFauxAuth(t, router, "/")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, callbackRequest("/auth/faux/callback?state="+state, cookies))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("callback status = %d; want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func TestAuthProviderInvalidProvider(t *testing.T) {
	router := newFauxAuthRouter(t, &fakeApiClient{})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/auth/bogus", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want %d", recorder.Code, http.StatusBadRequest)
	}
}

// AuthProvider 必须把 next 存进会话，供回调后跳回原页面。
func TestAuthProviderStoresNext(t *testing.T) {
	router := newFauxAuthRouter(t, &fakeApiClient{})
	router.GET("/probe", func(c *gin.Context) {
		sess := sessions.Default(c)
		next, _ := sess.Get(oauthNextKey("faux")).(string)
		c.String(http.StatusOK, next)
	})

	cookies, _ := beginFauxAuth(t, router, "/feed/x?tab=1")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, callbackRequest("/probe", cookies))
	if body := recorder.Body.String(); body != "/feed/x?tab=1" {
		t.Fatalf("stored next = %q; want %q", body, "/feed/x?tab=1")
	}
}

func TestLogoutHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(sessions.Sessions("ffdbsess", cookie.NewStore([]byte("test-secret"))))
	router.GET("/login", func(c *gin.Context) {
		sess := sessions.Default(c)
		sess.Set("user_id", "user-1")
		sess.Set("uuid", "uuid-1")
		if err := sess.Save(); err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/logout", LogoutHandler)
	handlerRan := false
	router.GET("/private", LoginRequired(), func(c *gin.Context) {
		handlerRan = true
		c.Status(http.StatusNoContent)
	})

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	cookies := loginRecorder.Result().Cookies()

	// next 跳转
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, callbackRequest("/logout?next=%2Ffeed%2Fx", cookies))
	if recorder.Code != http.StatusFound {
		t.Fatalf("logout status = %d; want %d", recorder.Code, http.StatusFound)
	}
	if location := recorder.Header().Get("Location"); location != "/feed/x" {
		t.Fatalf("Location = %q; want %q", location, "/feed/x")
	}

	// 登出后访问受保护页面会被重定向到登录
	handlerRan = false
	privateRecorder := httptest.NewRecorder()
	router.ServeHTTP(privateRecorder, callbackRequest("/private", recorder.Result().Cookies()))
	if handlerRan {
		t.Fatal("protected handler ran after logout")
	}
	if privateRecorder.Code != http.StatusFound {
		t.Fatalf("private status = %d; want %d", privateRecorder.Code, http.StatusFound)
	}

	// 外部地址的 next 被消毒为 "/"
	evilRecorder := httptest.NewRecorder()
	router.ServeHTTP(evilRecorder, callbackRequest("/logout?next=https%3A%2F%2Fevil.example%2Fx", cookies))
	if location := evilRecorder.Header().Get("Location"); location != "/" {
		t.Fatalf("Location = %q; want %q", location, "/")
	}
}

// 已登录（有 user_id）但 uuid 缺失的会话视为无效：强制登出并拦截请求。
func TestLoginRequiredMissingUuidForcesLogout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(sessions.Sessions("ffdbsess", cookie.NewStore([]byte("test-secret"))))
	router.GET("/login-uuidless", func(c *gin.Context) {
		sess := sessions.Default(c)
		sess.Set("user_id", "user-1")
		if err := sess.Save(); err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	handlerRan := false
	router.GET("/private", LoginRequired(), func(c *gin.Context) {
		handlerRan = true
		c.Status(http.StatusNoContent)
	})

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login-uuidless", nil))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, callbackRequest("/private", loginRecorder.Result().Cookies()))

	if handlerRan {
		t.Fatal("protected handler ran with a uuid-less session")
	}
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d; want %d", recorder.Code, http.StatusFound)
	}
	if location := recorder.Header().Get("Location"); location != "/" {
		t.Fatalf("Location = %q; want %q", location, "/")
	}
}
