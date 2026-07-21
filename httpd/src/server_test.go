package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
	if got := data["show_share"]; got != false {
		t.Fatalf("show_share = %#v; want false", got)
	}

	data = feedContext(&pb.Feed{Entries: make([]*pb.Entry, 30)}, 60, 30)
	if got := data["prev_start"]; got != int32(30) {
		t.Fatalf("prev_start = %#v; want 30", got)
	}
	if got := data["show_paging"]; got != false {
		t.Fatalf("show_paging = %#v; want false", got)
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
