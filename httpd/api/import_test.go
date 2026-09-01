package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type importAPIClient struct {
	*fakeAPIClient
	request          *pb.ImportFeedEntryRequest
	response         *pb.ImportFeedEntryResponse
	err              error
	metadata         metadata.MD
	calls            int
	operatorRequest  *pb.AuthenticateImportOperatorTokenRequest
	operatorResponse *pb.AuthenticateFeedApiKeyResponse
	operatorErr      error
}

func (f *importAPIClient) ImportFeedEntry(ctx context.Context, request *pb.ImportFeedEntryRequest, _ ...grpc.CallOption) (*pb.ImportFeedEntryResponse, error) {
	f.calls++
	f.request = request
	f.metadata, _ = metadata.FromOutgoingContext(ctx)
	return f.response, f.err
}

func (f *importAPIClient) AuthenticateImportOperatorToken(_ context.Context, request *pb.AuthenticateImportOperatorTokenRequest, _ ...grpc.CallOption) (*pb.AuthenticateFeedApiKeyResponse, error) {
	f.operatorRequest = request
	return f.operatorResponse, f.operatorErr
}

func importMetadataJSON(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(importMetadata{
		Source:      importSource{Kind: "twitter", AccountID: "12345", ItemID: "1295071681511407617", URL: "https://x.com/example/status/1295071681511407617"},
		PublishedAt: "2020-08-17T12:34:56Z", Title: "<b>Archive</b>",
		BodyHTML: `<p>old tweet</p><img src="https://remote.example/image.jpg">`,
	})
	require.NoError(t, err)
	return string(raw)
}

func importRequest(t *testing.T, values map[string]string, files []multipartPart) *http.Request {
	t.Helper()
	request, _ := multipartAPIRequest(t, values, files)
	request.URL.Path = "/api/v1/feed/imports"
	return request
}

func TestImportEntrySanitizesPromotesAndReturnsCreated(t *testing.T) {
	entry := canonicalPostResponse()
	entry.Via = &pb.Via{Name: "Twitter"}
	client := &importAPIClient{fakeAPIClient: validAPIClient(), response: &pb.ImportFeedEntryResponse{Entry: entry, Created: true}}
	request := importRequest(t, map[string]string{"metadata": importMetadataJSON(t)}, []multipartPart{
		{name: "file", filename: "photo.jpg", content: apiJPEG(t)},
	})
	recorder := httptest.NewRecorder()
	writeRouter(client, t.TempDir()).ServeHTTP(recorder, request)
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"created":true`)
	require.Equal(t, 1, client.calls)
	require.Equal(t, "Archive", client.request.Title)
	require.Equal(t, "<p>old tweet</p>", client.request.BodyHtml)
	require.Len(t, client.request.Thumbnails, 1)
	require.NotEmpty(t, client.metadata.Get("x-ff-feed-uuid"))
}

func TestImportEntryReplayReturnsOK(t *testing.T) {
	client := &importAPIClient{fakeAPIClient: validAPIClient(), response: &pb.ImportFeedEntryResponse{Entry: canonicalPostResponse()}}
	recorder := httptest.NewRecorder()
	writeRouter(client, t.TempDir()).ServeHTTP(recorder, importRequest(t, map[string]string{"metadata": importMetadataJSON(t)}, nil))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"created":false`)
}

func TestImportEntryOperatorTokenSelectsExplicitTargetBeforePromote(t *testing.T) {
	client := &importAPIClient{
		fakeAPIClient: &fakeAPIClient{err: status.Error(codes.Unauthenticated, "not a Feed key")},
		operatorResponse: &pb.AuthenticateFeedApiKeyResponse{
			FeedUuid: "22222222-2222-2222-2222-222222222222", KeyId: []byte("operator"),
		},
		response: &pb.ImportFeedEntryResponse{Entry: canonicalPostResponse(), Created: true},
	}
	request := importRequest(t, map[string]string{"metadata": importMetadataJSON(t)}, nil)
	request.Header.Set("Authorization", "Bearer operator-secret")
	request.Header.Set(operatorTargetHeader, "archive-target")
	recorder := httptest.NewRecorder()
	writeRouter(client, t.TempDir()).ServeHTTP(recorder, request)
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	require.Equal(t, "operator-secret", client.operatorRequest.Token)
	require.Equal(t, "archive-target", client.operatorRequest.TargetFeed)
	require.Equal(t, []string{"22222222-2222-2222-2222-222222222222"}, client.metadata.Get("x-ff-feed-uuid"))
}

func TestImportEntryRejectsTargetForFeedKeyAndMissingTargetForOperatorBeforeReadingBody(t *testing.T) {
	client := &importAPIClient{fakeAPIClient: validAPIClient(), response: &pb.ImportFeedEntryResponse{Entry: canonicalPostResponse()}}
	request := importRequest(t, map[string]string{"metadata": importMetadataJSON(t)}, nil)
	request.Header.Set(operatorTargetHeader, "forged")
	recorder := httptest.NewRecorder()
	writeRouter(client, t.TempDir()).ServeHTTP(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	client = &importAPIClient{fakeAPIClient: &fakeAPIClient{err: status.Error(codes.Unauthenticated, "not a Feed key")}}
	request = importRequest(t, map[string]string{"metadata": importMetadataJSON(t)}, nil)
	request.Header.Set("Authorization", "Bearer operator-secret")
	recorder = httptest.NewRecorder()
	writeRouter(client, t.TempDir()).ServeHTTP(recorder, request)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Nil(t, client.operatorRequest)
}

func TestImportEntryRejectsUnknownMetadataAndMapsConflict(t *testing.T) {
	client := &importAPIClient{fakeAPIClient: validAPIClient(), response: &pb.ImportFeedEntryResponse{Entry: canonicalPostResponse()}}
	bad := `{"source":{"kind":"twitter"},"published_at":"2020-01-01T00:00:00Z","body_html":"x","target_feed_uuid":"forged"}`
	recorder := httptest.NewRecorder()
	writeRouter(client, t.TempDir()).ServeHTTP(recorder, importRequest(t, map[string]string{"metadata": bad}, nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, client.calls)

	client.err = status.Error(codes.AlreadyExists, "internal collision details")
	recorder = httptest.NewRecorder()
	writeRouter(client, t.TempDir()).ServeHTTP(recorder, importRequest(t, map[string]string{"metadata": importMetadataJSON(t)}, nil))
	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), "source_identity_conflict")
	require.NotContains(t, recorder.Body.String(), "internal collision")
}
