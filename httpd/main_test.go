package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func TestEmbeddedJavaScriptContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	assets := fstest.MapFS{
		"static/js/bundle.min.js": {Data: []byte("console.log('ok');")},
	}
	router.GET("/static/*path", embeddedAssetHandler(assets))

	request := httptest.NewRequest(http.MethodGet, "/static/js/bundle.min.js", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("unexpected JavaScript content type %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("unexpected X-Content-Type-Options value %q", got)
	}
}

func TestMissingEmbeddedAssetReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/static/*path", embeddedAssetHandler(fstest.MapFS{}))

	request := httptest.NewRequest(http.MethodGet, "/static/js/missing.js", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.Code)
	}
}
