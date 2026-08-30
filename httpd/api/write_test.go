package api

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type writeAPIClient struct {
	*fakeAPIClient
	posted   *pb.Entry
	postResp *pb.Entry
	postErr  error
	metadata metadata.MD
	posts    int
}

func (f *writeAPIClient) PostEntry(ctx context.Context, request *pb.Entry, _ ...grpc.CallOption) (*pb.Entry, error) {
	f.posts++
	f.posted = request
	f.metadata, _ = metadata.FromOutgoingContext(ctx)
	return f.postResp, f.postErr
}

type multipartPart struct {
	name, filename string
	content        []byte
}

func multipartAPIRequest(t *testing.T, values map[string]string, files []multipartPart) (*http.Request, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range values {
		require.NoError(t, writer.WriteField(name, value))
	}
	for _, file := range files {
		part, err := writer.CreateFormFile(file.name, file.filename)
		require.NoError(t, err)
		_, err = part.Write(file.content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/feed/entries", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer valid-token")
	return request, body.String()
}

func apiJPEG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	require.NoError(t, jpeg.Encode(&output, image.NewRGBA(image.Rect(0, 0, 40, 20)), nil))
	return output.Bytes()
}

func writeRouter(client pb.ApiClient, root string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	New(client, media.NewUploadPipeline(&util.Config{MediaPath: root, MediaURL: "https://media.example"})).Register(router)
	return router
}

func canonicalPostResponse() *pb.Entry {
	return &pb.Entry{
		Id: "f145818e-e4ba-4d10-a499-515f46aac391", Title: "Reading notes",
		Body: "<p>A short note.</p>", Date: "2026-08-30T12:00:00.123456789Z",
		From: &pb.Feed{Id: "book-club", Name: "Book Club", Picture: "https://m.friendfeed.me/feed/book-club.jpg"},
		Via:  &pb.Via{Name: "FriendFeed API"},
	}
}

func TestPostEntryTextOnlySanitizesAndStripsExternalMedia(t *testing.T) {
	client := &writeAPIClient{fakeAPIClient: validAPIClient(), postResp: canonicalPostResponse()}
	request, _ := multipartAPIRequest(t, map[string]string{
		"title":     "<b>Reading notes</b>",
		"body_html": `<p>A short note.</p><img src="https://remote.example/x.jpg"><iframe src="https://evil.example"></iframe>`,
	}, nil)
	request.Header.Set("Idempotency-Key", "ignored-by-v1")
	recorder := httptest.NewRecorder()
	writeRouter(client, t.TempDir()).ServeHTTP(recorder, request)
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, client.posts)
	require.Equal(t, "Reading notes", client.posted.Title)
	require.Equal(t, "<p>A short note.</p>", client.posted.Body)
	require.NotContains(t, client.posted.Body, "remote.example")
	require.NotEmpty(t, client.metadata.Get("x-ff-feed-uuid"))
}

func TestPostEntryPublishesImageAndDocumentThroughSharedPipeline(t *testing.T) {
	client := &writeAPIClient{fakeAPIClient: validAPIClient(), postResp: canonicalPostResponse()}
	request, _ := multipartAPIRequest(t, nil, []multipartPart{
		{name: "file", filename: "photo.jpg", content: apiJPEG(t)},
		{name: "file", filename: "report.pdf", content: []byte("%PDF-1.7\nreport")},
	})
	recorder := httptest.NewRecorder()
	writeRouter(client, t.TempDir()).ServeHTTP(recorder, request)
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	require.Len(t, client.posted.Thumbnails, 1)
	require.Contains(t, client.posted.Thumbnails[0].Url, "https://media.example/")
	require.Len(t, client.posted.Files, 1)
	require.Equal(t, "application/pdf", client.posted.Files[0].Type)
	require.Contains(t, client.posted.Files[0].Url, "?download=")
}

func TestPostEntryRejectsUnknownFieldsAndUnsupportedFilesBeforeMutation(t *testing.T) {
	for _, testCase := range []struct {
		values map[string]string
		files  []multipartPart
		status int
	}{
		{values: map[string]string{"ProfileUuid": "forged", "body_html": "text"}, status: http.StatusBadRequest},
		{files: []multipartPart{{name: "file", filename: "payload.exe", content: []byte("MZ")}}, status: http.StatusUnsupportedMediaType},
		{files: []multipartPart{{name: "remote_url", filename: "x.jpg", content: apiJPEG(t)}}, status: http.StatusBadRequest},
	} {
		client := &writeAPIClient{fakeAPIClient: validAPIClient(), postResp: canonicalPostResponse()}
		request, _ := multipartAPIRequest(t, testCase.values, testCase.files)
		recorder := httptest.NewRecorder()
		writeRouter(client, t.TempDir()).ServeHTTP(recorder, request)
		require.Equal(t, testCase.status, recorder.Code, recorder.Body.String())
		require.Zero(t, client.posts)
	}
}
