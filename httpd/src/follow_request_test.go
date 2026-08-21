package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeFollowRequestClient stubs the follow-request RPCs; the embedded
// interface satisfies the rest (nil, never called).
type fakeFollowRequestClient struct {
	pb.ApiClient

	requestReq   *pb.RequestFollowRequest
	requestErr   error
	requestCalls int

	cancelErr   error
	cancelCalls int

	approveReq   *pb.FollowRequestAction
	approveErr   error
	approveCalls int

	rejectReq   *pb.FollowRequestAction
	rejectErr   error
	rejectCalls int
}

func (f *fakeFollowRequestClient) RequestFollow(ctx context.Context, req *pb.RequestFollowRequest, opts ...grpc.CallOption) (*pb.RequestFollowResponse, error) {
	f.requestCalls++
	f.requestReq = req
	return &pb.RequestFollowResponse{Requested: true}, f.requestErr
}

func (f *fakeFollowRequestClient) CancelFollowRequest(ctx context.Context, req *pb.RequestFollowRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	f.cancelCalls++
	return &emptypb.Empty{}, f.cancelErr
}

func (f *fakeFollowRequestClient) ApproveFollowRequest(ctx context.Context, req *pb.FollowRequestAction, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	f.approveCalls++
	f.approveReq = req
	return &emptypb.Empty{}, f.approveErr
}

func (f *fakeFollowRequestClient) RejectFollowRequest(ctx context.Context, req *pb.FollowRequestAction, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	f.rejectCalls++
	f.rejectReq = req
	return &emptypb.Empty{}, f.rejectErr
}

func followRequestTestRouter(s *Server) (*gin.Engine, *http.Cookie) {
	router := groupTestRouter(s)
	return router, nil
}

func TestFeedRequestHandlerFilesRequest(t *testing.T) {
	client := &fakeFollowRequestClient{}
	s := newGroupTestServer(client)
	router, _ := followRequestTestRouter(s)
	router.POST("/a/feed-request", s.FeedRequestHandler)
	login := groupLoginCookie(t, router)

	form := url.Values{"feed_uuid": {"feed-uuid-1"}, "feed_id": {"some-feed"}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/a/feed-request", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(login)
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/feed/some-feed" {
		t.Fatalf("unexpected redirect: %q", location)
	}
	if client.requestCalls != 1 {
		t.Fatalf("expected 1 RequestFollow call, got %d", client.requestCalls)
	}
	if client.requestReq.ActorUuid != testGroupUserUUID || client.requestReq.FeedUuid != "feed-uuid-1" {
		t.Fatalf("unexpected request: %+v", client.requestReq)
	}
}

func TestFeedRequestCancelHandler(t *testing.T) {
	client := &fakeFollowRequestClient{}
	s := newGroupTestServer(client)
	router, _ := followRequestTestRouter(s)
	router.POST("/a/feed-request/cancel", s.FeedRequestCancelHandler)
	login := groupLoginCookie(t, router)

	form := url.Values{"feed_uuid": {"feed-uuid-1"}, "feed_id": {"some-feed"}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/a/feed-request/cancel", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(login)
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if client.cancelCalls != 1 {
		t.Fatalf("expected 1 CancelFollowRequest call, got %d", client.cancelCalls)
	}
}

func TestAccountRequestActionApproveReject(t *testing.T) {
	client := &fakeFollowRequestClient{}
	s := newGroupTestServer(client)
	router, _ := followRequestTestRouter(s)
	router.POST("/account/requests/action", s.AccountRequestActionHandler)
	login := groupLoginCookie(t, router)

	post := func(action string) *httptest.ResponseRecorder {
		form := url.Values{"action": {action}, "target_uuid": {"target-uuid-1"}}
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/account/requests/action", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(login)
		router.ServeHTTP(recorder, req)
		return recorder
	}

	if recorder := post("approve"); recorder.Code != http.StatusFound {
		t.Fatalf("approve: expected 302, got %d", recorder.Code)
	}
	if client.approveCalls != 1 {
		t.Fatalf("expected 1 ApproveFollowRequest call, got %d", client.approveCalls)
	}
	// Approving against your own feed: actor and feed are both the session user.
	if client.approveReq.ActorUuid != testGroupUserUUID ||
		client.approveReq.FeedUuid != testGroupUserUUID ||
		client.approveReq.TargetUuid != "target-uuid-1" {
		t.Fatalf("unexpected approve request: %+v", client.approveReq)
	}

	if recorder := post("reject"); recorder.Code != http.StatusFound {
		t.Fatalf("reject: expected 302, got %d", recorder.Code)
	}
	if client.rejectCalls != 1 {
		t.Fatalf("expected 1 RejectFollowRequest call, got %d", client.rejectCalls)
	}

	if recorder := post("bogus"); recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown action: expected 400, got %d", recorder.Code)
	}
}
