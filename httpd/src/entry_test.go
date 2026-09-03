package server

import (
	"bytes"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
)

type uploadTestStorage struct {
	fetched []byte
	posts   []*media.Object
	postErr error
}

func (s *uploadTestStorage) Exists(string) (bool, error) { return false, nil }
func (s *uploadTestStorage) Fetch(obj *media.Object) (*http.Response, error) {
	obj.Content = append([]byte(nil), s.fetched...)
	return &http.Response{StatusCode: http.StatusOK}, nil
}
func (s *uploadTestStorage) Post(obj *media.Object) (*media.Object, error) {
	if s.postErr != nil {
		return obj, s.postErr
	}
	obj.Path = "a/b/object-" + string(rune('0'+len(s.posts)))
	s.posts = append(s.posts, obj)
	return obj, nil
}
func (s *uploadTestStorage) Thumbnail(obj *media.Object) (*media.Object, error) { return obj, nil }
func (s *uploadTestStorage) Mirror(obj *media.Object) (*media.Object, error)    { return obj, nil }
func (s *uploadTestStorage) FromUrl(string, string, string) (*media.Object, error) {
	return nil, nil
}

func uploadJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, jpeg.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height)), nil))
	return out.Bytes()
}

func multipartUpload(t *testing.T, content []byte, filename string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = file.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return &body, writer.FormDataContentType()
}

func TestEntryPostHandlerRejectsEditByNonAuthorBeforePosting(t *testing.T) {
	client := &fakeGroupClient{
		profile: &pb.Profile{Uuid: testGroupUserUUID, Id: "group-admin"},
		entryResp: &pb.Feed{Uuid: testGroupUUID, Entries: []*pb.Entry{{
			Id:          "33333333-3333-3333-3333-333333333333",
			ProfileUuid: "44444444-4444-4444-4444-444444444444",
			FeedUuid:    testGroupUUID,
			Body:        "original",
		}}},
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	router.POST("/a/share", s.EntryPostHandler)
	login := groupLoginCookie(t, router)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("id", client.entryResp.Entries[0].Id))
	require.NoError(t, writer.WriteField("feedUuid", testGroupUUID))
	require.NoError(t, writer.WriteField("body", "unauthorized edit"))
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/a/share", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(login)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Zero(t, client.postCalls)
}

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
	router.Use(sessions.Sessions("test", cookie.NewStore([]byte("secret"))))
	server := &Server{uploadRequests: make(chan struct{}, 8), imageOperations: make(chan struct{}, 2)}
	router.POST("/upload", server.UploadHandler)

	request := httptest.NewRequest(http.MethodPost, "/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d: %s", http.StatusRequestEntityTooLarge, response.Code, response.Body.String())
	}
}

func TestUploadHandlerValidatesBeforeStorageAndReturnsStagingURLs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	storage := &uploadTestStorage{}
	cfg := &util.Config{MediaPath: t.TempDir(), MediaURL: "https://media.example"}
	server := &Server{uploads: media.NewUploadPipeline(cfg), secretKey: "secret", uploadRequests: make(chan struct{}, 8), imageOperations: make(chan struct{}, 2)}
	server.uploadFetch = func(string) ([]byte, error) { return storage.fetched, nil }
	router := gin.New()
	router.Use(sessions.Sessions("test", cookie.NewStore([]byte("secret"))))
	router.POST("/upload", server.UploadHandler)

	badBody, badType := multipartUpload(t, []byte("not an image"), "fake.jpg")
	badRequest := httptest.NewRequest(http.MethodPost, "/upload", badBody)
	badRequest.Header.Set("Content-Type", badType)
	badResponse := httptest.NewRecorder()
	router.ServeHTTP(badResponse, badRequest)
	require.Equal(t, http.StatusUnprocessableEntity, badResponse.Code)
	require.Empty(t, storage.posts, "invalid bytes must not reach storage")

	goodBody, goodType := multipartUpload(t, uploadJPEG(t, 1400, 700), "photo.jpg")
	goodRequest := httptest.NewRequest(http.MethodPost, "/upload", goodBody)
	goodRequest.Header.Set("Content-Type", goodType)
	goodResponse := httptest.NewRecorder()
	router.ServeHTTP(goodResponse, goodRequest)
	require.Equal(t, http.StatusOK, goodResponse.Code, goodResponse.Body.String())
	require.Empty(t, storage.posts, "browser uploads stay local until publish")
	require.Contains(t, goodResponse.Body.String(), `"url":"https://media.example/upload-staging/`)
	require.Contains(t, goodResponse.Body.String(), `"originalUrl":"https://media.example/upload-staging/`)
	require.Contains(t, goodResponse.Body.String(), `"assetToken":`)
}

func TestUploadHandlerRemoteSourceUsesControlledFetchAndImagePipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	storage := &uploadTestStorage{fetched: uploadJPEG(t, 20, 10)}
	cfg := &util.Config{MediaPath: t.TempDir()}
	server := &Server{uploads: media.NewUploadPipeline(cfg), secretKey: "secret", uploadRequests: make(chan struct{}, 8), imageOperations: make(chan struct{}, 2)}
	server.uploadFetch = func(string) ([]byte, error) { return storage.fetched, nil }
	router := gin.New()
	router.Use(sessions.Sessions("test", cookie.NewStore([]byte("secret"))))
	router.POST("/upload", server.UploadHandler)

	form := url.Values{"sourceUrl": {"https://images.example/photo.jpg"}}
	request := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Empty(t, storage.posts)
}

func TestUploadHandlerRejectsInvalidRemoteURLBeforeFetch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{uploads: media.NewUploadPipeline(&util.Config{MediaPath: t.TempDir()}), secretKey: "secret", uploadRequests: make(chan struct{}, 8), imageOperations: make(chan struct{}, 2)}
	called := false
	server.uploadFetch = func(string) ([]byte, error) { called = true; return nil, nil }
	router := gin.New()
	router.Use(sessions.Sessions("test", cookie.NewStore([]byte("secret"))))
	router.POST("/upload", server.UploadHandler)
	form := url.Values{"sourceUrl": {"file:///etc/passwd"}}
	request := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.False(t, called)
}
