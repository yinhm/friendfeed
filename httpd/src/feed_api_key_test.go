package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
)

type fakeFeedApiKeyClient struct {
	pb.ApiClient
	feed       *pb.Feed
	profile    *pb.Profile
	status     *pb.FeedApiKeyStatusResponse
	mutation   *pb.FeedApiKeyMutationResponse
	manageReq  *pb.FeedApiKeyManageRequest
	actionCall string
}

func (f *fakeFeedApiKeyClient) FetchProfile(context.Context, *pb.ProfileRequest, ...grpc.CallOption) (*pb.Profile, error) {
	return f.profile, nil
}

func (f *fakeFeedApiKeyClient) Command(context.Context, *pb.CommandRequest, ...grpc.CallOption) (*pb.CommandResponse, error) {
	return &pb.CommandResponse{Result: `{"version":1}`}, nil
}

func (f *fakeFeedApiKeyClient) FetchFeed(context.Context, *pb.FeedRequest, ...grpc.CallOption) (*pb.Feed, error) {
	return f.feed, nil
}

func (f *fakeFeedApiKeyClient) GetFeedApiKeyStatus(_ context.Context, req *pb.FeedApiKeyManageRequest, _ ...grpc.CallOption) (*pb.FeedApiKeyStatusResponse, error) {
	f.manageReq = req
	return f.status, nil
}

func (f *fakeFeedApiKeyClient) GenerateFeedApiKey(_ context.Context, req *pb.FeedApiKeyManageRequest, _ ...grpc.CallOption) (*pb.FeedApiKeyMutationResponse, error) {
	f.manageReq, f.actionCall = req, "generate"
	return f.mutation, nil
}

func TestFeedApiKeyPageBootstrapNeverContainsToken(t *testing.T) {
	client := &fakeFeedApiKeyClient{
		feed:    &pb.Feed{Uuid: testGroupUserUUID, Id: "renamed-user", Name: "Renamed User", Type: "user"},
		profile: &pb.Profile{Uuid: testGroupUserUUID, Id: "renamed-user", Name: "Renamed User", Type: "user"},
		status:  &pb.FeedApiKeyStatusResponse{FeedUuid: testGroupUserUUID, KeyId: []byte("12345678"), Active: true},
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	capture := &captureNotificationRender{}
	router.HTMLRender = capture
	router.GET("/feed/:name/api", s.FeedApiKeyPageHandler)
	login := groupLoginCookie(t, router)
	req := httptest.NewRequest(http.MethodGet, "/feed/renamed-user/api", nil)
	req.AddCookie(login)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)
	raw := capture.data["pageBootstrap"].(string)
	require.NotContains(t, raw, "ffk1_")
	var bootstrap struct {
		Page string             `json:"page"`
		Data feedApiKeyPageData `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &bootstrap))
	require.Equal(t, "feed-api-key", bootstrap.Page)
	require.Equal(t, "renamed-user", bootstrap.Data.Feed.ID)
	require.True(t, bootstrap.Data.Status.Active)
	require.Equal(t, testGroupUserUUID, client.manageReq.ActorUuid)
}

func TestFeedApiKeyGenerateReturnsTokenOnlyInNoStoreMutationResponse(t *testing.T) {
	client := &fakeFeedApiKeyClient{
		feed:    &pb.Feed{Uuid: testGroupUserUUID, Id: "renamed-user", Type: "user"},
		profile: &pb.Profile{Uuid: testGroupUserUUID, Id: "renamed-user", Type: "user"},
		mutation: &pb.FeedApiKeyMutationResponse{
			Status: &pb.FeedApiKeyStatusResponse{FeedUuid: testGroupUserUUID, KeyId: []byte("12345678"), Active: true},
			Token:  "ffk1_one-time-token",
		},
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	router.POST("/feed/:name/api/:action", s.FeedApiKeyActionHandler)
	login := groupLoginCookie(t, router)
	req := httptest.NewRequest(http.MethodPost, "/feed/renamed-user/api/generate", nil)
	req.AddCookie(login)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Contains(t, recorder.Body.String(), "ffk1_one-time-token")
	require.Equal(t, "generate", client.actionCall)
	require.Equal(t, testGroupUserUUID, client.manageReq.ActorUuid)
}
