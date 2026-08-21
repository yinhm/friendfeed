package server

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDefaultFeedPictures(t *testing.T) {
	feed := &pb.Feed{Entries: []*pb.Entry{
		{From: &pb.Feed{}},
		{From: &pb.Feed{Picture: "  "}},
		{From: &pb.Feed{Picture: "https://example.com/a.png"}},
		{}, // nil From must not panic
		nil,
	}}

	defaultFeedPictures(feed)

	if feed.Picture != DefaultPictureURL {
		t.Fatalf("feed picture = %q; want %q", feed.Picture, DefaultPictureURL)
	}
	for i, want := range []string{DefaultPictureURL, DefaultPictureURL, "https://example.com/a.png"} {
		if got := feed.Entries[i].From.Picture; got != want {
			t.Fatalf("entry %d From.Picture = %q; want %q", i, got, want)
		}
	}
}

func TestSanitizeFeedEntries(t *testing.T) {
	feed := &pb.Feed{Entries: []*pb.Entry{{
		Body: `<p>safe<script>alert(1)</script>` +
			`<a href="https://example.com" onclick="alert(2)">link</a>` +
			`<a href="javascript:alert(3)">unsafe link</a>` +
			`<img src="https://example.com/image.png" onerror="alert(4)"></p>` +
			`<ul><li>item</li></ul><blockquote>quote</blockquote>`,
	}, nil}}

	sanitizeFeedEntries(feed)

	got := feed.Entries[0].Body
	if strings.Contains(got, "<script") || strings.Contains(got, "onerror") ||
		strings.Contains(got, "onclick") || strings.Contains(got, "javascript:") {
		t.Fatalf("unsafe entry body returned as JSON: %q", got)
	}
	for _, safe := range []string{
		`<a href="https://example.com"`,
		`<img src="https://example.com/image.png"`,
		"<ul>",
		"<li>item</li>",
		"<blockquote>quote</blockquote>",
	} {
		if !strings.Contains(got, safe) {
			t.Fatalf("sanitized entry body lost %q: %q", safe, got)
		}
	}
}

func TestParseAssetManifest(t *testing.T) {
	raw := []byte(`{
		"src/index.jsx": {"file": "static/js/bundle-a1b2c3.min.js", "isEntry": true},
		"style.css": {"file": "static/css/bundle-d4e5f6.min.css", "src": "style.css"}
	}`)

	jsFile, cssFile, err := parseAssetManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if jsFile != "static/js/bundle-a1b2c3.min.js" {
		t.Fatalf("jsFile = %q", jsFile)
	}
	if cssFile != "static/css/bundle-d4e5f6.min.css" {
		t.Fatalf("cssFile = %q", cssFile)
	}

	if _, _, err := parseAssetManifest([]byte(`{"other": {"file": "x.js"}}`)); err == nil {
		t.Fatal("expected an error for a manifest without entry assets")
	}
}

func TestFingerprint(t *testing.T) {
	got := fingerprint([]byte("body{}"))
	if len(got) != 8 {
		t.Fatalf("fingerprint length = %d; want 8", len(got))
	}
	if fingerprint([]byte("changed")) == got {
		t.Fatal("fingerprint did not change with content")
	}
}

func TestEntryTitle(t *testing.T) {
	tests := []struct {
		name  string
		entry *pb.Entry
		want  string
	}{
		{
			name:  "body fallback strips all tags",
			entry: &pb.Entry{Body: `<p>plain <strong>bold</strong> <code>x()</code> <em>em</em> text</p>`},
			want:  "plain bold x() em text",
		},
		{
			name:  "explicit title is stripped too",
			entry: &pb.Entry{Title: `<b>titled</b> post`, Body: "ignored"},
			want:  "titled post",
		},
		{
			name:  "truncates at 42 runes",
			entry: &pb.Entry{Title: "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopq"},
			want:  "abcdefghijklmnopqrstuvwxyzabcdefghijklmnop",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := entryTitle(test.entry); got != test.want {
				t.Fatalf("entryTitle() = %q; want %q", got, test.want)
			}
		})
	}
}

func TestFirstEntry(t *testing.T) {
	tests := []struct {
		name string
		feed *pb.Feed
	}{
		{name: "nil feed"},
		{name: "empty feed", feed: &pb.Feed{}},
		{name: "nil entry", feed: &pb.Feed{Entries: []*pb.Entry{nil}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry, err := firstEntry(test.feed)
			if entry != nil {
				t.Fatalf("entry = %#v; want nil", entry)
			}
			if status.Code(err) != codes.NotFound {
				t.Fatalf("error code = %s; want %s", status.Code(err), codes.NotFound)
			}
		})
	}

	want := &pb.Entry{Id: "entry-id"}
	got, err := firstEntry(&pb.Feed{Entries: []*pb.Entry{want}})
	if err != nil {
		t.Fatalf("firstEntry() error = %v", err)
	}
	if got != want {
		t.Fatalf("firstEntry() = %#v; want %#v", got, want)
	}
}

func TestFeedContext(t *testing.T) {
	feed := &pb.Feed{
		Id:      "public",
		Entries: make([]*pb.Entry, 31),
	}
	data := feedContext(feed, 15, 30)

	if got := data["feed"]; got != feed {
		t.Fatalf("feed = %#v; want %#v", got, feed)
	}
	if got := data["title"]; got != feed.Id {
		t.Fatalf("title = %#v; want %q", got, feed.Id)
	}
	if got := data["name"]; got != feed.Id {
		t.Fatalf("name = %#v; want %q", got, feed.Id)
	}
	if got := data["prev_start"]; got != int32(0) {
		t.Fatalf("prev_start = %#v; want 0", got)
	}
	if got := data["next_start"]; got != int32(45) {
		t.Fatalf("next_start = %#v; want 45", got)
	}
	if got := data["show_paging"]; got != true {
		t.Fatalf("show_paging = %#v; want true", got)
	}
	if got := data["show_prev"]; got != true {
		t.Fatalf("show_prev = %#v; want true", got)
	}
	if got := data["show_next"]; got != true {
		t.Fatalf("show_next = %#v; want true", got)
	}
	if got := len(feed.Entries); got != 30 {
		t.Fatalf("rendered entries = %d; want 30", got)
	}
	if got := data["show_share"]; got != false {
		t.Fatalf("show_share = %#v; want false", got)
	}

	// A short last page has no lookahead, but Prev must stay reachable.
	data = feedContext(&pb.Feed{Entries: make([]*pb.Entry, 30)}, 60, 30)
	if got := data["prev_start"]; got != int32(30) {
		t.Fatalf("prev_start = %#v; want 30", got)
	}
	if got := data["show_prev"]; got != true {
		t.Fatalf("show_prev = %#v; want true", got)
	}
	if got := data["show_next"]; got != false {
		t.Fatalf("show_next = %#v; want false", got)
	}
	if got := data["show_paging"]; got != true {
		t.Fatalf("show_paging = %#v; want true", got)
	}

	// The first page never shows Prev, even when more pages follow.
	data = feedContext(&pb.Feed{Entries: make([]*pb.Entry, 31)}, 0, 30)
	if got := data["show_prev"]; got != false {
		t.Fatalf("show_prev = %#v; want false", got)
	}
	if got := data["show_next"]; got != true {
		t.Fatalf("show_next = %#v; want true", got)
	}
}

func TestCursorFeedContext(t *testing.T) {
	feed := &pb.Feed{Id: "profile", NextCursor: "older"}
	data := cursorFeedContext(feed)

	if got := data["cursor_paging"]; got != true {
		t.Fatalf("cursor_paging = %#v; want true", got)
	}
	if got := data["next_cursor"]; got != "older" {
		t.Fatalf("next_cursor = %#v; want older", got)
	}
	if got := data["show_paging"]; got != true {
		t.Fatalf("show_paging = %#v; want true", got)
	}

	data = cursorFeedContext(&pb.Feed{Id: "profile"})
	if got := data["show_paging"]; got != false {
		t.Fatalf("show_paging = %#v; want false", got)
	}
}

func TestConfigureFeedPaginationPreservesLegacyStartLinks(t *testing.T) {
	tests := []struct {
		name       string
		rawQuery   string
		wantCursor bool
		wantStart  int32
		wantValue  string
	}{
		{name: "new request defaults to cursor", wantCursor: true},
		{name: "legacy start", rawQuery: "start=60", wantStart: 60},
		{name: "explicit zero start", rawQuery: "start=0"},
		{name: "cursor wins over start", rawQuery: "start=60&cursor=opaque", wantCursor: true, wantValue: "opaque"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/feed/user?"+test.rawQuery, nil)
			req := &pb.FeedRequest{PageSize: 30}
			if got := configureFeedPagination(r, req); got != test.wantCursor {
				t.Fatalf("configureFeedPagination() = %v; want %v", got, test.wantCursor)
			}
			if req.CursorPaging != test.wantCursor {
				t.Fatalf("CursorPaging = %v; want %v", req.CursorPaging, test.wantCursor)
			}
			if req.Start != test.wantStart {
				t.Fatalf("Start = %d; want %d", req.Start, test.wantStart)
			}
			if req.Cursor != test.wantValue {
				t.Fatalf("Cursor = %q; want %q", req.Cursor, test.wantValue)
			}
		})
	}
}

func TestCommentDeleteHandlerRejectsInvalidForm(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := &Server{}
	router := gin.New()
	router.POST("/comment/delete", server.CommentDeleteHandler)

	request := httptest.NewRequest(
		http.MethodPost,
		"/comment/delete",
		strings.NewReader("entry=entry-id"),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestRequestErrorReturnsRetryableTimelineInitialization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	handled := RequestError(ctx, status.Error(codes.Unavailable, pb.HomeTimelineInitializing))

	if !handled {
		t.Fatal("RequestError did not handle initializing response")
	}
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d; want %d", recorder.Code, http.StatusAccepted)
	}
	if got := recorder.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q; want 2", got)
	}
	if got := recorder.Header().Get("Refresh"); got != "2" {
		t.Fatalf("Refresh = %q; want 2", got)
	}
}

func TestRequestErrorReturnsForbiddenForPermissionDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.SetHTMLTemplate(template.Must(template.New("403.html").Parse("forbidden")))
	engine.GET("/private", func(ctx *gin.Context) {
		RequestError(ctx, status.Error(codes.PermissionDenied, "private"))
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/private", nil)
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want %d", recorder.Code, http.StatusForbidden)
	}
}

// fakeFollowHandlerClient stubs GraphFollow; the embedded interface satisfies
// the rest (nil, never called).
type fakeFollowHandlerClient struct {
	pb.ApiClient

	followResp *pb.FollowResponse
	followErr  error
}

func (f *fakeFollowHandlerClient) GraphFollow(ctx context.Context, req *pb.FollowRequest, opts ...grpc.CallOption) (*pb.FollowResponse, error) {
	return f.followResp, f.followErr
}

// A domain rejection (e.g. a Group admin leaving before demotion) must reach
// the browser as a JSON body with the server's reason, not a bare 500 that
// response.json() cannot parse.
func TestFollowHandlerReturnsJSONErrorForDomainRejection(t *testing.T) {
	client := &fakeFollowHandlerClient{
		followErr: status.Error(codes.FailedPrecondition, "Group admin must be demoted before membership can be removed"),
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	router.POST("/a/follow", s.FollowHandler)
	login := groupLoginCookie(t, router)

	recorder := postForm(t, router, "/a/follow", url.Values{
		"feed_uuid": {"feed-uuid"}, "action": {"unfollow"},
	}, login)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d; want %d: %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v: %q", err, recorder.Body.String())
	}
	if body["error"] != "Group admin must be demoted before membership can be removed" {
		t.Fatalf("error message = %q", body["error"])
	}
}
