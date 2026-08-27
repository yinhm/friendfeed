package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLocalMediaForcesActiveContentToDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "upload-staging"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "upload-staging", "asset.html"), []byte("<script>alert(1)</script>"), 0644))
	router := gin.New()
	router.GET("/file/*filepath", localMediaHandler(root))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/file/upload-staging/asset.html", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/octet-stream", response.Header().Get("Content-Type"))
	require.Equal(t, "attachment", response.Header().Get("Content-Disposition"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
}

func TestLocalMediaStillServesRasterImagesInline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "image.png"), []byte("not relevant to header policy"), 0644))
	router := gin.New()
	router.GET("/file/*filepath", localMediaHandler(root))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/file/image.png", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Empty(t, response.Header().Get("Content-Disposition"))
}
