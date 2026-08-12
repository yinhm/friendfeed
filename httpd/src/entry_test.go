package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestUploadHandlerRejectsOversizedRequest(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "large.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(make([]byte, maxUploadRequestBytes)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	server := &Server{}
	router.POST("/upload", server.UploadHandler)

	request := httptest.NewRequest(http.MethodPost, "/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d: %s", http.StatusRequestEntityTooLarge, response.Code, response.Body.String())
	}
}

func TestNewEntryUUIDDistinguishesSameSecondPosts(t *testing.T) {
	profile := "4e580875-46c3-58fe-a436-bcc17d7e2509"
	base := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)

	first := newEntryUUID(profile, base)
	if got := newEntryUUID(profile, base); got != first {
		t.Fatalf("newEntryUUID is not deterministic: %s != %s", got, first)
	}
	for _, offset := range []time.Duration{time.Nanosecond, time.Millisecond, time.Second} {
		if got := newEntryUUID(profile, base.Add(offset)); got == first {
			t.Fatalf("posts at %s offset share UUID %s", offset, first)
		}
	}
}
