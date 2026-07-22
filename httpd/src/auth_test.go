package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
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
			router.Use(sessions.Sessions("test", sessions.NewCookieStore([]byte("test-secret"))))
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
