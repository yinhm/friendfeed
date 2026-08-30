package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type readAPIClient struct {
	*fakeAPIClient
	feed      *pb.Feed
	entryFeed *pb.Feed
	feedErr   error
	entryErr  error
	feedReq   *pb.FeedRequest
	entryReq  *pb.EntryRequest
	metadata  metadata.MD
}

func (f *readAPIClient) FetchFeed(ctx context.Context, request *pb.FeedRequest, _ ...grpc.CallOption) (*pb.Feed, error) {
	f.feedReq = request
	f.metadata, _ = metadata.FromOutgoingContext(ctx)
	return f.feed, f.feedErr
}

func (f *readAPIClient) FetchEntry(ctx context.Context, request *pb.EntryRequest, _ ...grpc.CallOption) (*pb.Feed, error) {
	f.entryReq = request
	f.metadata, _ = metadata.FromOutgoingContext(ctx)
	return f.entryFeed, f.entryErr
}

func goldenFeed() *pb.Feed {
	return &pb.Feed{
		Uuid: "6b7043ef-9a56-4f88-89b6-683d31cf7081", Id: "book-club", Name: "Book Club",
		Description: "Books and discussion", Picture: "https://m.friendfeed.me/feed/book-club.jpg",
		Type: "group", Private: true,
	}
}

func goldenEntry() *pb.Entry {
	return &pb.Entry{
		Id: "f145818e-e4ba-4d10-a499-515f46aac391", Title: "Reading notes",
		Body: "<p>A short note.</p>", Date: "2026-08-30T12:00:00.123456789Z",
		From: &pb.Feed{Id: "book-club", Name: "Book Club", Picture: "https://m.friendfeed.me/feed/book-club.jpg"},
		Via:  &pb.Via{Name: "FriendFeed API"},
	}
}

func publicAPITestRouter(client pb.ApiClient) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	New(client).Register(router)
	return router
}

func publicAPIRequest(router http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func requireGoldenJSON(t *testing.T, name string, actual string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "public_api_v1", name+".json"))
	require.NoError(t, err)
	require.JSONEq(t, string(raw), actual)
}

func TestGetFeedMatchesGoldenAndAuthenticatesOnce(t *testing.T) {
	client := &readAPIClient{fakeAPIClient: validAPIClient(), feed: goldenFeed()}
	recorder := publicAPIRequest(publicAPITestRouter(client), "/api/v1/feed")
	require.Equal(t, http.StatusOK, recorder.Code)
	requireGoldenJSON(t, "feed", recorder.Body.String())
	calls, _ := client.observed()
	require.Equal(t, 1, calls)
	require.Equal(t, client.response.FeedUuid, client.feedReq.ProfileUuid)
	require.True(t, client.feedReq.CursorPaging)
	require.Empty(t, client.feedReq.ViewerUuid)
	require.NotEmpty(t, client.metadata.Get("x-ff-feed-uuid"))
}

func TestListEntriesMatchesGoldenAndPassesOpaqueCursor(t *testing.T) {
	feed := goldenFeed()
	feed.Entries = []*pb.Entry{goldenEntry()}
	client := &readAPIClient{fakeAPIClient: validAPIClient(), feed: feed}
	recorder := publicAPIRequest(publicAPITestRouter(client), "/api/v1/feed/entries?limit=1&cursor=opaque")
	require.Equal(t, http.StatusOK, recorder.Code)
	requireGoldenJSON(t, "list", recorder.Body.String())
	require.Equal(t, int32(1), client.feedReq.PageSize)
	require.Equal(t, "opaque", client.feedReq.Cursor)
}

func TestGetEntryMatchesGolden(t *testing.T) {
	feed := goldenFeed()
	feed.Entries = []*pb.Entry{goldenEntry()}
	client := &readAPIClient{fakeAPIClient: validAPIClient(), entryFeed: feed}
	recorder := publicAPIRequest(publicAPITestRouter(client), "/api/v1/feed/entries/f145818e-e4ba-4d10-a499-515f46aac391")
	require.Equal(t, http.StatusOK, recorder.Code)
	requireGoldenJSON(t, "entry", recorder.Body.String())
	require.Equal(t, "f145818e-e4ba-4d10-a499-515f46aac391", client.entryReq.Uuid)
}

func TestReadAPIRejectsLegacyAndInvalidPagination(t *testing.T) {
	client := &readAPIClient{fakeAPIClient: validAPIClient(), feed: goldenFeed()}
	for _, path := range []string{
		"/api/v1/feed/entries?start=10", "/api/v1/feed/entries?limit=0", "/api/v1/feed/entries?limit=101",
	} {
		recorder := publicAPIRequest(publicAPITestRouter(client), path)
		require.Equal(t, http.StatusBadRequest, recorder.Code, path)
		require.Contains(t, recorder.Body.String(), `"code":"invalid_request"`)
	}
}

func TestReadAPIMasksCrossFeedEntryAndInternalErrors(t *testing.T) {
	client := &readAPIClient{fakeAPIClient: validAPIClient(), entryErr: status.Error(codes.PermissionDenied, "target mismatch secret detail")}
	recorder := publicAPIRequest(publicAPITestRouter(client), "/api/v1/feed/entries/other")
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "mismatch")

	client = &readAPIClient{fakeAPIClient: validAPIClient(), feedErr: status.Error(codes.InvalidArgument, "cursor internals")}
	recorder = publicAPIRequest(publicAPITestRouter(client), "/api/v1/feed/entries?cursor=bad")
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "internals")
}
