package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc"
)

func TestAttachmentTokenIsBoundToActorExpiryAndSignature(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	payload := attachmentTokenPayload{
		Actor: "actor", Key: strings.Repeat("a", 1) + "/" + strings.Repeat("b", 1) + "/" + strings.Repeat("c", 62),
		Name: "report.pdf", MimeType: "application/pdf", Size: 10, Expires: now.Add(time.Hour).Unix(),
	}
	token, err := signAttachmentToken("secret", payload)
	require.NoError(t, err)
	got, err := verifyAttachmentToken("secret", token, "actor", now)
	require.NoError(t, err)
	require.Equal(t, payload.Name, got.Name)

	_, err = verifyAttachmentToken("secret", token, "other", now)
	require.Error(t, err)
	_, err = verifyAttachmentToken("secret", token, "actor", now.Add(2*time.Hour))
	require.Error(t, err)
	_, err = verifyAttachmentToken("secret", token+"x", "actor", now)
	require.Error(t, err)
}

func TestFilesForEntryPostPreservesExplicitExistingAndAddsToken(t *testing.T) {
	now := time.Now().UTC()
	server := &Server{secretKey: "secret"}
	entry := &pb.Entry{Id: "entry-id", Files: []*pb.File{
		{Url: "https://legacy.example/keep.pdf", Name: "keep.pdf", Size: 3},
		{Url: "https://legacy.example/remove.pdf", Name: "remove.pdf", Size: 4},
	}}
	payload := attachmentTokenPayload{
		Actor: "actor", Key: "a/b/" + strings.Repeat("c", 62), Name: "new.pdf",
		MimeType: "application/pdf", Size: 5, Expires: now.Add(time.Hour).Unix(),
	}
	token, err := signAttachmentToken("secret", payload)
	require.NoError(t, err)
	files, err := server.filesForEntryPost("actor", entry, true,
		[]string{"https://legacy.example/keep.pdf"}, []string{token, token}, now)
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, "keep.pdf", files[0].Name)
	require.Equal(t, "/e/entry-id/files/ab"+strings.Repeat("c", 62)+"/new.pdf", files[1].Url)
}

type downloadAPIClient struct {
	pb.ApiClient
	feed *pb.Feed
}

func (f *downloadAPIClient) FetchEntry(context.Context, *pb.EntryRequest, ...grpc.CallOption) (*pb.Feed, error) {
	return f.feed, nil
}

func TestDownloadFileHandlerForcesAttachmentAndSupportsRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &util.Config{MediaPath: t.TempDir()}
	local := media.NewLocalStorage(cfg, 1024)
	obj := &media.Object{Content: []byte("0123456789")}
	_, err := local.Post(obj)
	require.NoError(t, err)
	digest, err := digestFromObjectKey(obj.Path)
	require.NoError(t, err)
	entryID := "11111111-1111-1111-1111-111111111111"
	fileURL := attachmentDownloadURL(entryID, digest, "report.html")
	server := &Server{
		client: &downloadAPIClient{feed: &pb.Feed{Entries: []*pb.Entry{{
			Id: entryID, Files: []*pb.File{{Url: fileURL, Name: "report.html", Size: 10}},
		}}}},
		localMedia: local,
	}
	router := gin.New()
	router.Use(sessions.Sessions("ffdbsess", cookie.NewStore([]byte("test-secret"))))
	router.GET("/e/:uuid/files/:digest/:name", server.DownloadFileHandler)
	request := httptest.NewRequest(http.MethodGet, fileURL, nil)
	request.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusPartialContent, response.Code, response.Body.String())
	require.Equal(t, "2345", response.Body.String())
	require.Equal(t, "application/octet-stream", response.Header().Get("Content-Type"))
	require.Contains(t, response.Header().Get("Content-Disposition"), "attachment")
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
}
