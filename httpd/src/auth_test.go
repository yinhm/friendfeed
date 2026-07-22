package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	gsessions "github.com/gorilla/sessions"
	"github.com/markbates/goth/gothic"
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
