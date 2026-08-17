package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"
	"github.com/patrickmn/go-cache"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const testGroupUserUUID = "11111111-1111-1111-1111-111111111111"
const testGroupUUID = "22222222-2222-2222-2222-222222222222"

// fakeGroupClient stubs the RPCs the Group handlers use; the embedded
// interface satisfies the rest (nil, never called).
type fakeGroupClient struct {
	pb.ApiClient

	createReq   *pb.CreateGroupRequest
	createResp  *pb.Profile
	createErr   error
	createCalls int

	groupsResp  *pb.ListUserGroupsResponse
	groupPages  []*pb.ListUserGroupsResponse
	groupsErr   error
	groupsCalls int
	groupsReq   *pb.ListUserGroupsRequest

	feedResp *pb.Feed
	feedErr  error

	groupView    *pb.GroupView
	groupViewErr error

	profile *pb.Profile

	membersResp *pb.ListGroupMembersResponse
	membersErr  error

	updateReq *pb.UpdateGroupRequest
	updateErr error

	deleteErr   error
	deleteCalls int

	membershipReq *pb.GroupMembershipRequest
	membershipErr error
	promoteCalls  int
	demoteCalls   int
	removeCalls   int
}

func (f *fakeGroupClient) CreateGroup(ctx context.Context, req *pb.CreateGroupRequest, opts ...grpc.CallOption) (*pb.Profile, error) {
	f.createCalls++
	f.createReq = req
	return f.createResp, f.createErr
}

func (f *fakeGroupClient) ListUserGroups(ctx context.Context, req *pb.ListUserGroupsRequest, opts ...grpc.CallOption) (*pb.ListUserGroupsResponse, error) {
	f.groupsCalls++
	f.groupsReq = req
	if len(f.groupPages) >= f.groupsCalls {
		return f.groupPages[f.groupsCalls-1], f.groupsErr
	}
	if f.groupsResp == nil {
		return &pb.ListUserGroupsResponse{}, f.groupsErr
	}
	return f.groupsResp, f.groupsErr
}

func TestAllUserGroupsConsumesEveryCursorPageAndSorts(t *testing.T) {
	client := &fakeGroupClient{groupPages: []*pb.ListUserGroupsResponse{
		{Groups: []*pb.Profile{{Name: "Zulu"}}, NextCursor: "next"},
		{Groups: []*pb.Profile{{Name: "alpha"}}},
	}}
	s := newGroupTestServer(client)
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	groups, err := s.allUserGroups(ctx, testGroupUserUUID)
	if err != nil {
		t.Fatalf("allUserGroups: %v", err)
	}
	if client.groupsCalls != 2 || len(groups) != 2 || groups[0].Name != "alpha" || groups[1].Name != "Zulu" {
		t.Fatalf("groups=%v calls=%d", groups, client.groupsCalls)
	}
}

func (f *fakeGroupClient) FetchFeed(ctx context.Context, req *pb.FeedRequest, opts ...grpc.CallOption) (*pb.Feed, error) {
	return f.feedResp, f.feedErr
}

func (f *fakeGroupClient) GetGroup(ctx context.Context, req *pb.GetGroupRequest, opts ...grpc.CallOption) (*pb.GroupView, error) {
	return f.groupView, f.groupViewErr
}

func (f *fakeGroupClient) FetchProfile(ctx context.Context, req *pb.ProfileRequest, opts ...grpc.CallOption) (*pb.Profile, error) {
	return f.profile, nil
}

func (f *fakeGroupClient) ListGroupMembers(ctx context.Context, req *pb.ListGroupMembersRequest, opts ...grpc.CallOption) (*pb.ListGroupMembersResponse, error) {
	return f.membersResp, f.membersErr
}

func (f *fakeGroupClient) UpdateGroup(ctx context.Context, req *pb.UpdateGroupRequest, opts ...grpc.CallOption) (*pb.Profile, error) {
	f.updateReq = req
	return nil, f.updateErr
}

func (f *fakeGroupClient) DeleteGroup(ctx context.Context, req *pb.DeleteGroupRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	f.deleteCalls++
	return &emptypb.Empty{}, f.deleteErr
}

func (f *fakeGroupClient) AddGroupAdmin(ctx context.Context, req *pb.GroupMembershipRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	f.promoteCalls++
	f.membershipReq = req
	return &emptypb.Empty{}, f.membershipErr
}

func (f *fakeGroupClient) RemoveGroupAdmin(ctx context.Context, req *pb.GroupMembershipRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	f.demoteCalls++
	f.membershipReq = req
	return &emptypb.Empty{}, f.membershipErr
}

func (f *fakeGroupClient) RemoveGroupMember(ctx context.Context, req *pb.GroupMembershipRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	f.removeCalls++
	f.membershipReq = req
	return &emptypb.Empty{}, f.membershipErr
}

// nopRender lets handlers that answer with c.HTML run without a template FS.
type nopRender struct{}

func (nopRender) Instance(name string, data any) render.Render {
	return render.Data{ContentType: "text/html; charset=utf-8", Data: []byte("nop")}
}

func newGroupTestServer(client pb.ApiClient) *Server {
	return &Server{
		client: client,
		cache:  cache.New(5*time.Minute, 10*time.Minute),
	}
}

// groupTestRouter installs the session middleware and a helper route that
// logs the test user in, so handlers see a real CurrentUserUuid.
func groupTestRouter(s *Server) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = nopRender{}
	store := cookie.NewStore([]byte("group-test-secret"))
	router.Use(sessions.Sessions("ffdbsess", store))
	router.GET("/test-login", func(c *gin.Context) {
		sess := sessions.Default(c)
		sess.Set("user_id", "test-user")
		sess.Set("uuid", testGroupUserUUID)
		if err := sess.Save(); err != nil {
			c.String(http.StatusInternalServerError, "session save failed")
			return
		}
		c.String(http.StatusOK, "ok")
	})
	return router
}

func groupLoginCookie(t *testing.T, router *gin.Engine) *http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test-login", nil))
	for _, c := range recorder.Result().Cookies() {
		if c.Name == "ffdbsess" {
			return c
		}
	}
	t.Fatal("login did not set a session cookie")
	return nil
}

func postForm(t *testing.T, router *gin.Engine, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestGroupCreateHandlerRequiresIDAndName(t *testing.T) {
	client := &fakeGroupClient{}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	router.POST("/groups/create", s.GroupCreateHandler)

	// The nop renderer answers "nop" for every template; what matters here
	// is the status code and that validation stops before the RPC.
	recorder := postForm(t, router, "/groups/create", url.Values{"name": {"Book Club"}})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing id: status = %d; want 400", recorder.Code)
	}

	recorder = postForm(t, router, "/groups/create", url.Values{"id": {"book-club"}})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing name: status = %d; want 400", recorder.Code)
	}
	if client.createCalls != 0 {
		t.Fatal("CreateGroup must not be called when validation fails")
	}
}

func TestGroupCreateHandlerPassesID(t *testing.T) {
	client := &fakeGroupClient{createResp: &pb.Profile{Uuid: testGroupUUID, Id: "book-club"}}
	s := newGroupTestServer(client)

	router := groupTestRouter(s)
	router.POST("/groups/create", s.GroupCreateHandler)
	login := groupLoginCookie(t, router)

	recorder := postForm(t, router, "/groups/create", url.Values{
		"id":          {"book-club"},
		"name":        {"Book Club"},
		"description": {"reading"},
		"picture":     {"https://example.com/p.png"},
	}, login)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d (%s); want 303", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/feed/book-club" {
		t.Fatalf("redirect Location = %q; want /feed/book-club", location)
	}
	req := client.createReq
	if req == nil {
		t.Fatal("CreateGroup was not called")
	}
	if req.ActorUuid != testGroupUserUUID || req.Id != "book-club" ||
		req.Name != "Book Club" || req.Description != "reading" ||
		req.Picture != "https://example.com/p.png" {
		t.Fatalf("CreateGroup request = %+v", req)
	}
}

func TestGroupCreateHandlerEchoesServerError(t *testing.T) {
	client := &fakeGroupClient{
		createErr: status.Error(codes.FailedPrecondition, `Group ID "book-club" is already taken`),
		// CurrentUser needs a profile once the error path renders the form.
		profile: &pb.Profile{Uuid: testGroupUserUUID, Id: "test-user"},
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	router.POST("/groups/create", s.GroupCreateHandler)
	login := groupLoginCookie(t, router)

	recorder := postForm(t, router, "/groups/create", url.Values{
		"id": {"book-club"}, "name": {"Book Club"},
	}, login)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", recorder.Code)
	}
}

func TestUserGroupsPreservesActivityOrderWithoutStaleCache(t *testing.T) {
	client := &fakeGroupClient{groupsResp: &pb.ListUserGroupsResponse{Groups: []*pb.Profile{
		{Name: "beta"}, {Name: "Alpha"}, {Name: "Gamma"},
	}}}
	s := newGroupTestServer(client)

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	groups, err := s.UserGroups(ctx, testGroupUserUUID)
	if err != nil {
		t.Fatalf("UserGroups: %v", err)
	}
	got := []string{groups[0].Name, groups[1].Name, groups[2].Name}
	want := []string{"beta", "Alpha", "Gamma"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("activity order = %v; want %v", got, want)
		}
	}

	if _, err := s.UserGroups(ctx, testGroupUserUUID); err != nil {
		t.Fatalf("second UserGroups: %v", err)
	}
	if client.groupsCalls != 2 {
		t.Fatalf("ListUserGroups called %d times; want 2 (live materialized view)", client.groupsCalls)
	}
	if client.groupsReq == nil || !client.groupsReq.OrderByActivity || client.groupsReq.Limit != 10 {
		t.Fatalf("sidebar request = %+v; want activity order with limit 10", client.groupsReq)
	}
}

func TestCanManageGroup(t *testing.T) {
	admin := &pb.GroupView{IsAdmin: true}
	plain := &pb.GroupView{}
	super := &pb.Profile{IsSuper: true}
	user := &pb.Profile{}

	if !canManageGroup(admin, user) {
		t.Fatal("group admin must manage")
	}
	if canManageGroup(plain, user) {
		t.Fatal("plain member must not manage")
	}
	if !canManageGroup(plain, super) {
		t.Fatal("super must manage (GetGroup is_admin excludes supers)")
	}
	if canManageGroup(nil, super) {
		t.Fatal("nil view must not manage")
	}
}

func TestGroupMemberActionRemove(t *testing.T) {
	client := &fakeGroupClient{
		feedResp:  &pb.Feed{Uuid: testGroupUUID, Id: "book-club"},
		groupView: &pb.GroupView{Group: &pb.Profile{Uuid: testGroupUUID, Id: "book-club"}, IsAdmin: true},
		profile:   &pb.Profile{Uuid: testGroupUserUUID},
	}
	s := newGroupTestServer(client)
	target := "33333333-3333-3333-3333-333333333333"

	router := groupTestRouter(s)
	router.POST("/groups/:name/members/action", s.GroupMemberActionHandler)
	login := groupLoginCookie(t, router)

	recorder := postForm(t, router, "/groups/book-club/members/action", url.Values{
		"action": {"remove"}, "target_uuid": {target},
	}, login)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d (%s); want 302", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Location"); got != "/groups/book-club/members" {
		t.Fatalf("Location = %q", got)
	}
	if client.removeCalls != 1 {
		t.Fatalf("RemoveGroupMember called %d times; want 1", client.removeCalls)
	}
	req := client.membershipReq
	if req.ActorUuid != testGroupUserUUID || req.GroupUuid != testGroupUUID || req.TargetUuid != target {
		t.Fatalf("membership request = %+v", req)
	}
}

func TestGroupMemberActionRejectsUnknownAction(t *testing.T) {
	client := &fakeGroupClient{
		feedResp:  &pb.Feed{Uuid: testGroupUUID, Id: "book-club"},
		groupView: &pb.GroupView{Group: &pb.Profile{Uuid: testGroupUUID, Id: "book-club"}},
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	router.POST("/groups/:name/members/action", s.GroupMemberActionHandler)
	login := groupLoginCookie(t, router)

	recorder := postForm(t, router, "/groups/book-club/members/action", url.Values{
		"action": {"explode"}, "target_uuid": {"33333333-3333-3333-3333-333333333333"},
	}, login)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", recorder.Code)
	}
	if client.promoteCalls+client.demoteCalls+client.removeCalls != 0 {
		t.Fatal("no membership RPC must run for an unknown action")
	}
}

func TestGroupSettingsHandlerSubmitsAllFields(t *testing.T) {
	client := &fakeGroupClient{
		feedResp:  &pb.Feed{Uuid: testGroupUUID, Id: "book-club"},
		groupView: &pb.GroupView{Group: &pb.Profile{Uuid: testGroupUUID, Id: "book-club"}, IsAdmin: true},
		profile:   &pb.Profile{Uuid: testGroupUserUUID},
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	router.POST("/groups/:name/settings", s.GroupSettingsHandler)
	login := groupLoginCookie(t, router)

	recorder := postForm(t, router, "/groups/book-club/settings", url.Values{
		"name":        {"Book Club!"},
		"description": {"new desc"},
		"picture":     {"https://example.com/new.png"},
	}, login)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d (%s); want 302", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Location"); got != "/feed/book-club" {
		t.Fatalf("Location = %q", got)
	}
	req := client.updateReq
	if req == nil {
		t.Fatal("UpdateGroup was not called")
	}
	if req.ActorUuid != testGroupUserUUID || req.GroupUuid != testGroupUUID ||
		req.Name != "Book Club!" || req.Description != "new desc" ||
		req.Picture != "https://example.com/new.png" {
		t.Fatalf("UpdateGroup request = %+v", req)
	}
}

func TestGroupSettingsHandlerForbiddenForPlainMember(t *testing.T) {
	client := &fakeGroupClient{
		feedResp:  &pb.Feed{Uuid: testGroupUUID, Id: "book-club"},
		groupView: &pb.GroupView{Group: &pb.Profile{Uuid: testGroupUUID, Id: "book-club"}},
		profile:   &pb.Profile{Uuid: testGroupUserUUID},
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	router.GET("/groups/:name/settings", s.GroupSettingsPageHandler)
	login := groupLoginCookie(t, router)

	req := httptest.NewRequest(http.MethodGet, "/groups/book-club/settings", nil)
	req.AddCookie(login)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want 403", recorder.Code)
	}
}

func TestGroupSettingsPageAllowsSuper(t *testing.T) {
	client := &fakeGroupClient{
		feedResp:  &pb.Feed{Uuid: testGroupUUID, Id: "book-club"},
		groupView: &pb.GroupView{Group: &pb.Profile{Uuid: testGroupUUID, Id: "book-club"}},
		profile:   &pb.Profile{Uuid: testGroupUserUUID, IsSuper: true},
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	router.GET("/groups/:name/settings", s.GroupSettingsPageHandler)
	login := groupLoginCookie(t, router)

	req := httptest.NewRequest(http.MethodGet, "/groups/book-club/settings", nil)
	req.AddCookie(login)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", recorder.Code)
	}
}

func TestGroupDeleteHandler(t *testing.T) {
	client := &fakeGroupClient{
		feedResp:  &pb.Feed{Uuid: testGroupUUID, Id: "book-club"},
		groupView: &pb.GroupView{Group: &pb.Profile{Uuid: testGroupUUID, Id: "book-club"}, IsAdmin: true},
		profile:   &pb.Profile{Uuid: testGroupUserUUID},
	}
	s := newGroupTestServer(client)

	router := groupTestRouter(s)
	router.POST("/groups/:name/delete", s.GroupDeleteHandler)
	login := groupLoginCookie(t, router)

	recorder := postForm(t, router, "/groups/book-club/delete", url.Values{}, login)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d (%s); want 302", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Location"); got != "/" {
		t.Fatalf("Location = %q; want /", got)
	}
	if client.deleteCalls != 1 {
		t.Fatalf("DeleteGroup called %d times; want 1", client.deleteCalls)
	}
}
