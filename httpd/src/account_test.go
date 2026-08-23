package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
)

// fakeAccountClient stubs only the two RPCs fetchAccountData uses; the
// embedded interface satisfies the rest (nil, never called).
type fakeAccountClient struct {
	pb.ApiClient

	profile     *pb.Profile
	profileErr  error
	profileWait time.Duration

	services     *pb.ListFeedServicesResponse
	servicesErr  error
	servicesWait time.Duration
	servicesReq  *pb.ListFeedServicesRequest
	feed         *pb.Feed
	feedErr      error
}

func (f *fakeAccountClient) FetchProfile(ctx context.Context, req *pb.ProfileRequest, opts ...grpc.CallOption) (*pb.Profile, error) {
	if f.profileWait > 0 {
		time.Sleep(f.profileWait)
	}
	return f.profile, f.profileErr
}

func (f *fakeAccountClient) ListFeedServices(ctx context.Context, req *pb.ListFeedServicesRequest, opts ...grpc.CallOption) (*pb.ListFeedServicesResponse, error) {
	f.servicesReq = req
	if f.servicesWait > 0 {
		time.Sleep(f.servicesWait)
	}
	return f.services, f.servicesErr
}

func (f *fakeAccountClient) FetchFeed(ctx context.Context, req *pb.FeedRequest, opts ...grpc.CallOption) (*pb.Feed, error) {
	return f.feed, f.feedErr
}

func (f *fakeAccountClient) ListUserGroups(ctx context.Context, req *pb.ListUserGroupsRequest, opts ...grpc.CallOption) (*pb.ListUserGroupsResponse, error) {
	return &pb.ListUserGroupsResponse{}, nil
}

func (f *fakeAccountClient) Command(ctx context.Context, req *pb.CommandRequest, opts ...grpc.CallOption) (*pb.CommandResponse, error) {
	return &pb.CommandResponse{Result: `{"version":1,"unread_count":0,"total_count":0}`}, nil
}

func TestFetchAccountData(t *testing.T) {
	profile := &pb.Profile{Uuid: "u1", Id: "yinhm"}
	response := &pb.ListFeedServicesResponse{Services: []*pb.FeedService{{Id: "twitter", Name: "Twitter"}}}
	client := &fakeAccountClient{profile: profile, services: response}

	gotProfile, services, err := fetchAccountData(client, "u1", "u1")
	if err != nil {
		t.Fatalf("fetchAccountData: %v", err)
	}
	if gotProfile != profile {
		t.Errorf("profile = %v; want the fetched profile", gotProfile)
	}
	if len(services.Services) != 1 || services.Services[0].Name != "Twitter" {
		t.Errorf("services = %v; want the twitter service", services)
	}
}

func TestFetchAccountDataNilServices(t *testing.T) {
	client := &fakeAccountClient{
		profile:  &pb.Profile{Uuid: "u1"},
		services: &pb.ListFeedServicesResponse{},
	}
	_, services, err := fetchAccountData(client, "u1", "u1")
	if err != nil {
		t.Fatalf("fetchAccountData: %v", err)
	}
	if services == nil || len(services.Services) != 0 {
		t.Errorf("services = %v; want normalized empty map", services)
	}
}

func TestFetchAccountDataServicesFailure(t *testing.T) {
	client := &fakeAccountClient{
		profile:     &pb.Profile{Uuid: "u1"},
		servicesErr: errors.New("services rpc down"),
	}
	if _, _, err := fetchAccountData(client, "u1", "u1"); err == nil {
		t.Fatal("want error when ListFeedServices fails")
	}
}

func TestFeedImportPageUsesFeedIDAndAuthorizedTarget(t *testing.T) {
	client := &fakeAccountClient{
		profile:  &pb.Profile{Uuid: testGroupUserUUID, Id: "test-user", Type: "user"},
		feed:     &pb.Feed{Uuid: testGroupUUID, Id: "book-club", Name: "Book Club", Type: "group"},
		services: &pb.ListFeedServicesResponse{},
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	capture := new(captureNotificationRender)
	router.HTMLRender = capture
	router.GET("/feed/:name/import", LoginRequired(), s.FeedImportPageHandler)
	cookie := groupLoginCookie(t, router)
	req := httptest.NewRequest(http.MethodGet, "/feed/book-club/import", nil)
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d; want 200", response.Code)
	}
	if client.servicesReq == nil || client.servicesReq.ActorUuid != testGroupUserUUID ||
		client.servicesReq.TargetFeedUuid != testGroupUUID {
		t.Fatalf("ListFeedServices request=%+v", client.servicesReq)
	}
	if capture.data["feed_management_page"] != "import" ||
		capture.data["manage_services_url"] != "/feed/book-club/import" {
		t.Fatalf("feed import context=%v", capture.data)
	}
}

func TestFetchAccountDataProfileFailure(t *testing.T) {
	client := &fakeAccountClient{
		profileErr: errors.New("profile rpc down"),
		services:   &pb.ListFeedServicesResponse{},
	}
	if _, _, err := fetchAccountData(client, "u1", "u1"); err == nil {
		t.Fatal("want error when FetchProfile fails")
	}
}

// Two slow-but-legal RPCs must overlap: serial calls each taking ~500ms
// would sum to a second, parallel calls stay near the slower one.
func TestFetchAccountDataParallel(t *testing.T) {
	client := &fakeAccountClient{
		profile:      &pb.Profile{Uuid: "u1"},
		profileWait:  500 * time.Millisecond,
		services:     &pb.ListFeedServicesResponse{},
		servicesWait: 500 * time.Millisecond,
	}

	start := time.Now()
	if _, _, err := fetchAccountData(client, "u1", "u1"); err != nil {
		t.Fatalf("fetchAccountData: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 900*time.Millisecond {
		t.Errorf("took %v; want parallel fetches (well under 1s)", elapsed)
	}
}
