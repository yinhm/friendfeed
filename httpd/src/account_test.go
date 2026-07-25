package server

import (
	"context"
	"errors"
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

	graph     *pb.Graph
	graphErr  error
	graphWait time.Duration
}

func (f *fakeAccountClient) FetchProfile(ctx context.Context, req *pb.ProfileRequest, opts ...grpc.CallOption) (*pb.Profile, error) {
	if f.profileWait > 0 {
		time.Sleep(f.profileWait)
	}
	return f.profile, f.profileErr
}

func (f *fakeAccountClient) FetchGraph(ctx context.Context, req *pb.ProfileRequest, opts ...grpc.CallOption) (*pb.Graph, error) {
	if f.graphWait > 0 {
		time.Sleep(f.graphWait)
	}
	return f.graph, f.graphErr
}

func TestFetchAccountData(t *testing.T) {
	profile := &pb.Profile{Uuid: "u1", Id: "yinhm"}
	graph := &pb.Graph{
		Services: map[string]*pb.Service{
			"twitter": {Id: "twitter", Name: "Twitter"},
		},
	}
	client := &fakeAccountClient{profile: profile, graph: graph}

	gotProfile, services, err := fetchAccountData(client, "u1")
	if err != nil {
		t.Fatalf("fetchAccountData: %v", err)
	}
	if gotProfile != profile {
		t.Errorf("profile = %v; want the fetched profile", gotProfile)
	}
	if len(services) != 1 || services["twitter"].Name != "Twitter" {
		t.Errorf("services = %v; want the twitter service", services)
	}
}

func TestFetchAccountDataNilServices(t *testing.T) {
	client := &fakeAccountClient{
		profile: &pb.Profile{Uuid: "u1"},
		graph:   &pb.Graph{}, // Services nil
	}
	_, services, err := fetchAccountData(client, "u1")
	if err != nil {
		t.Fatalf("fetchAccountData: %v", err)
	}
	if services == nil || len(services) != 0 {
		t.Errorf("services = %v; want normalized empty map", services)
	}
}

func TestFetchAccountDataGraphFailure(t *testing.T) {
	client := &fakeAccountClient{
		profile:  &pb.Profile{Uuid: "u1"},
		graphErr: errors.New("graph rpc down"),
	}
	if _, _, err := fetchAccountData(client, "u1"); err == nil {
		t.Fatal("want error when FetchGraph fails")
	}
}

func TestFetchAccountDataProfileFailure(t *testing.T) {
	client := &fakeAccountClient{
		profileErr: errors.New("profile rpc down"),
		graph:      &pb.Graph{},
	}
	if _, _, err := fetchAccountData(client, "u1"); err == nil {
		t.Fatal("want error when FetchProfile fails")
	}
}

// Two slow-but-legal RPCs must overlap: serial calls each taking ~500ms
// would sum to a second, parallel calls stay near the slower one.
func TestFetchAccountDataParallel(t *testing.T) {
	client := &fakeAccountClient{
		profile:     &pb.Profile{Uuid: "u1"},
		profileWait: 500 * time.Millisecond,
		graph:       &pb.Graph{},
		graphWait:   500 * time.Millisecond,
	}

	start := time.Now()
	if _, _, err := fetchAccountData(client, "u1"); err != nil {
		t.Fatalf("fetchAccountData: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 900*time.Millisecond {
		t.Errorf("took %v; want parallel fetches (well under 1s)", elapsed)
	}
}
