package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type interactionFeedClient struct {
	pb.ApiClient
	feedRequest        *pb.FeedRequest
	interactionRequest *pb.InteractionFeedRequest
}

func (f *interactionFeedClient) FetchFeed(_ context.Context, req *pb.FeedRequest, _ ...grpc.CallOption) (*pb.Feed, error) {
	f.feedRequest = req
	return &pb.Feed{Uuid: testGroupUserUUID, Id: "private-owner", Private: true}, nil
}

func (f *interactionFeedClient) FetchInteractionFeed(_ context.Context, req *pb.InteractionFeedRequest, _ ...grpc.CallOption) (*pb.InteractionFeedResponse, error) {
	f.interactionRequest = req
	return nil, status.Error(codes.Unavailable, "stop after authorization requests")
}

func (f *interactionFeedClient) FetchProfile(_ context.Context, _ *pb.ProfileRequest, _ ...grpc.CallOption) (*pb.Profile, error) {
	return &pb.Profile{Uuid: testGroupUserUUID, Id: "private-owner", Private: true}, nil
}

func (f *interactionFeedClient) FetchGraph(_ context.Context, _ *pb.ProfileRequest, _ ...grpc.CallOption) (*pb.Graph, error) {
	return new(pb.Graph), nil
}

func TestRenamedFeedLocation(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		feed      *pb.Feed
		query     string
		want      string
		redirect  bool
	}{
		{
			name:      "renamed feed preserves query",
			requested: "old-name",
			feed:      &pb.Feed{Id: "new-name"},
			query:     "start=30",
			want:      "/feed/new-name?start=30",
			redirect:  true,
		},
		{
			name:      "canonical feed does not redirect",
			requested: "same-name",
			feed:      &pb.Feed{Id: "same-name"},
		},
		{
			name:      "nil feed does not redirect",
			requested: "old-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, redirect := renamedFeedLocation(tt.requested, tt.feed, tt.query)
			if got != tt.want || redirect != tt.redirect {
				t.Fatalf("renamedFeedLocation() = %q, %t; want %q, %t",
					got, redirect, tt.want, tt.redirect)
			}
		})
	}
}

func TestInteractionFeedUsesNormalFeedFormattingWithoutFilteringInteractions(t *testing.T) {
	entry := &pb.Entry{
		Id: "entry", Date: time.Now().UTC().Format(time.RFC3339),
		Likes: make([]*pb.Like, 6), Comments: make([]*pb.Comment, 6),
	}
	for i := range entry.Likes {
		entry.Likes[i] = &pb.Like{From: &pb.Feed{Id: fmt.Sprintf("like-%d", i)}}
		entry.Comments[i] = &pb.Comment{Id: fmt.Sprintf("comment-%d", i), From: &pb.Feed{Id: fmt.Sprintf("commenter-%d", i)}}
	}
	response := &pb.InteractionFeedResponse{
		Profile: &pb.Profile{Uuid: "profile", Id: "owner"},
		Items: []*pb.InteractionItem{{
			Entry: entry,
			// This identifies the indexed interaction, but must not replace the
			// complete Like list loaded on Entry.
			Like: entry.Likes[5],
		}},
	}

	feed := interactionFeedForDisplay(response, &pb.Profile{}, &pb.Graph{})
	if len(feed.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(feed.Entries))
	}
	got := feed.Entries[0]
	if len(got.Likes) != 4 || !got.Likes[3].Placeholder || got.Likes[3].Num != 3 {
		t.Fatalf("likes were not formatted from the complete list: %+v", got.Likes)
	}
	if len(got.Comments) != 3 || !got.Comments[1].Placeholder || got.Comments[1].Num != 4 {
		t.Fatalf("comments were not formatted from the complete list: %+v", got.Comments)
	}
}

func TestRenamedInteractionFeedLocationPreservesSuffixAndQuery(t *testing.T) {
	got, redirect := renamedFeedLocationWithSuffix(
		"old-name",
		&pb.Feed{Id: "new-name"},
		"comments",
		"cursor=abc",
	)
	if !redirect || got != "/feed/new-name/comments?cursor=abc" {
		t.Fatalf("renamedFeedLocationWithSuffix() = %q, %t", got, redirect)
	}
	got, redirect = renamedFeedLocationWithSuffix("old-name", &pb.Feed{Id: "new-name"}, "groups", "")
	if !redirect || got != "/feed/new-name/groups" {
		t.Fatalf("renamed groups location = %q, %t", got, redirect)
	}
}

func TestInteractionFeedPassesOwnerIdentityToPrivateFeedLookup(t *testing.T) {
	client := new(interactionFeedClient)
	server := newGroupTestServer(client)
	router := groupTestRouter(server)
	router.GET("/feed/:name/likes", server.InteractionFeedHandler(
		pb.InteractionKind_INTERACTION_KIND_LIKE, "likes",
	))
	cookie := groupLoginCookie(t, router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/feed/private-owner/likes", nil)
	request.AddCookie(cookie)
	router.ServeHTTP(recorder, request)

	if client.feedRequest == nil {
		t.Fatal("private feed lookup was not called")
	}
	if client.feedRequest.ViewerUuid != testGroupUserUUID {
		t.Fatalf("private feed lookup viewer_uuid = %q; want %q", client.feedRequest.ViewerUuid, testGroupUserUUID)
	}
	if client.interactionRequest == nil {
		t.Fatal("interaction feed lookup was not called")
	}
	if client.interactionRequest.ViewerUuid != testGroupUserUUID {
		t.Fatalf("interaction feed viewer_uuid = %q; want %q", client.interactionRequest.ViewerUuid, testGroupUserUUID)
	}
}
