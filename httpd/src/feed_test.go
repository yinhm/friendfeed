package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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

func TestAnonymousFeedStartRedirectsToFirstPage(t *testing.T) {
	client := new(fakeGroupClient)
	server := newGroupTestServer(client)
	router := groupTestRouter(server)
	router.GET("/feed/:name", server.FeedHandler)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/feed/alice?cursor=ignored&start=30&source=bot", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status=%d; want 302", recorder.Code)
	}
	if got := recorder.Header().Get("Location"); got != "/feed/alice?source=bot" {
		t.Fatalf("Location=%q; want anonymous first page", got)
	}
	if client.feedCalls != 0 {
		t.Fatalf("FetchFeed calls=%d; anonymous offset must not reach ffdb", client.feedCalls)
	}
}

func TestLoggedInFeedStartMayRenderCompatibilityPage(t *testing.T) {
	server := newGroupTestServer(new(fakeGroupClient))
	router := groupTestRouter(server)
	router.GET("/feed/:name", func(c *gin.Context) {
		if redirectAnonymousLegacyFeedStart(c) {
			return
		}
		c.Status(http.StatusNoContent)
	})
	login := groupLoginCookie(t, router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/feed/alice?start=30", nil)
	request.AddCookie(login)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status=%d; logged-in legacy page should be allowed", recorder.Code)
	}
}

func TestLegacyFeedPageContinuesWithCursor(t *testing.T) {
	feed := &pb.Feed{Id: "alice", NextCursor: "next-cursor"}
	for i := 0; i < 31; i++ {
		feed.Entries = append(feed.Entries, &pb.Entry{Id: fmt.Sprintf("entry-%d", i)})
	}
	data := legacyFeedCursorContext(feed, 30)

	if len(feed.Entries) != 30 {
		t.Fatalf("entries=%d; want lookahead trimmed", len(feed.Entries))
	}
	if got := data["cursor_paging"]; got != true {
		t.Fatalf("cursor_paging=%v; want true", got)
	}
	if got := data["next_cursor"]; got != "next-cursor" {
		t.Fatalf("next_cursor=%v; want next-cursor", got)
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

func TestCollapseThumbnailImages(t *testing.T) {
	thumbs := []*pb.Thumbnail{
		{Url: "https://media.example/a/b/thumb.jpg", Link: "https://media.example/a/b/original.jpg"},
	}

	// Uploaded image: the anchor wrapping the thumbnail is removed entirely.
	body := `<p>before</p><p><a href="https://media.example/a/b/original.jpg"><img src="https://media.example/a/b/thumb.jpg" alt=""/></a></p><p>after</p>`
	got := collapseThumbnailImages(body, thumbs)
	if strings.Contains(got, "<img") || strings.Contains(got, "<a") {
		t.Fatalf("expected thumbnail image and its anchor removed, got %q", got)
	}
	for _, kept := range []string{"before", "after"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("expected %q preserved, got %q", kept, got)
		}
	}

	// An unmatched remote image stays inline.
	body = `<p>text</p><p><img src="https://remote.example/pic.png"/></p>`
	if got := collapseThumbnailImages(body, thumbs); !strings.Contains(got, `https://remote.example/pic.png`) {
		t.Fatalf("unmatched image must stay inline, got %q", got)
	}

	// Paragraphs left visually empty — by the collapse or by the editor's
	// zero-width image spacers — are dropped from list bodies.
	body = `<p>text</p>` +
		`<p><a href="https://media.example/a/b/original.jpg"><img src="https://media.example/a/b/thumb.jpg"/></a></p>` +
		"<p><span><span>\ufeff</span></span></p>" +
		`<p> </p>` +
		`<p>tail</p>`
	got = collapseThumbnailImages(body, thumbs)
	if strings.Contains(got, "<img") {
		t.Fatalf("collapsed image must be gone, got %q", got)
	}
	if strings.Count(got, "<p>") != 2 || !strings.Contains(got, "text") || !strings.Contains(got, "tail") {
		t.Fatalf("empty paragraphs must be dropped, got %q", got)
	}

	// No thumbnails: the body is untouched.
	if got := collapseThumbnailImages(body, nil); got != body {
		t.Fatal("body without thumbnails must stay unchanged")
	}
}

func TestPrepareFeedEntryPermalinkHidesThumbnailMediaBox(t *testing.T) {
	newEntry := func() *pb.Entry {
		return &pb.Entry{
			Id:         "entry-id",
			Date:       "2026-08-27T00:00:00Z",
			Body:       `<p><a href="https://media.example/o.jpg"><img src="https://media.example/t.jpg"/></a></p>`,
			Thumbnails: []*pb.Thumbnail{{Url: "https://media.example/t.jpg", Link: "https://media.example/o.jpg"}},
		}
	}

	permalink := newEntry()
	prepareFeedEntry(permalink, nil, nil, false)
	if permalink.Thumbnails != nil {
		t.Fatal("permalink must not render thumbnails already inline in the body")
	}
	if !strings.Contains(permalink.Body, "<img") {
		t.Fatalf("permalink keeps the full body, got %q", permalink.Body)
	}

	// Legacy entries keep their images in thumbnails alone; the body has no
	// inline image, so the media box must stay.
	legacy := newEntry()
	legacy.Body = `<p>archived text without images</p>`
	prepareFeedEntry(legacy, nil, nil, false)
	if len(legacy.Thumbnails) != 1 {
		t.Fatalf("permalink keeps thumbnails not present in the body, got %v", legacy.Thumbnails)
	}

	// A mixed entry keeps only the thumbnails missing from the body.
	mixed := newEntry()
	mixed.Thumbnails = append(mixed.Thumbnails, &pb.Thumbnail{Url: "https://media.example/other.jpg"})
	prepareFeedEntry(mixed, nil, nil, false)
	if len(mixed.Thumbnails) != 1 || mixed.Thumbnails[0].Url != "https://media.example/other.jpg" {
		t.Fatalf("permalink keeps only the non-inline thumbnail, got %v", mixed.Thumbnails)
	}

	list := newEntry()
	prepareFeedEntry(list, nil, nil, true)
	if list.Thumbnails == nil {
		t.Fatal("list pages keep the thumbnails for the media box")
	}
	if strings.Contains(list.Body, "<img") {
		t.Fatalf("list body must drop the thumbnail-backed image, got %q", list.Body)
	}
}

func TestPostedEntryViewUsesRequestedPresentationWithoutMutatingStoredBody(t *testing.T) {
	originalBody := `<p>hello</p><a href="https://media.example/original.jpg"><img src="https://media.example/thumb.jpg"></a>`
	entry := &pb.Entry{
		Id: "entry", Date: "2026-08-28T12:00:00Z", Body: originalBody,
		Thumbnails: []*pb.Thumbnail{{Url: "https://media.example/thumb.jpg", Link: "https://media.example/original.jpg"}},
	}

	list := postedEntryView(entry, nil, nil, "list")
	if strings.Contains(list.Body, "<img") || len(list.Thumbnails) != 1 {
		t.Fatalf("list response must use the thumbnail box: %+v", list)
	}
	permalink := postedEntryView(entry, nil, nil, "permalink")
	if !strings.Contains(permalink.Body, "<img") || len(permalink.Thumbnails) != 0 {
		t.Fatalf("permalink response must keep the inline image without a duplicate thumbnail: %+v", permalink)
	}
	if entry.Body != originalBody || len(entry.Thumbnails) != 1 {
		t.Fatal("response presentation mutated the canonical RPC entry")
	}
}
