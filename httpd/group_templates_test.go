package main

import (
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flosch/pongo2"
	"github.com/yinhm/friendfeed/pb"
)

// renderEmbeddedTemplate compiles name (with its layout.html parent chain)
// from the embedded templates and executes it with ctx.
func renderEmbeddedTemplate(t *testing.T, name string, ctx pongo2.Context) string {
	t.Helper()
	templateFS, err := fs.Sub(assetsFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewFriendRender(templateFS, false)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if err := renderer.Instance(name, ctx).Render(response); err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	return response.Body.String()
}

func TestFeedArchiveSidebarRendersOnlyWhenSnapshotExists(t *testing.T) {
	currentUser := &pb.Profile{Uuid: "11111111-1111-1111-1111-111111111111", Id: "me"}
	feed := &pb.Feed{Uuid: "22222222-2222-2222-2222-222222222222", Id: "archive", Name: "Archive"}
	base := pongo2.Context{
		"title": "Archive", "current_user": currentUser, "feed": feed,
		"show_header": true, "feed_archive_id": feed.Id,
	}

	without := renderEmbeddedTemplate(t, "feed.html", base)
	if strings.Contains(without, "feed-archive-menu") {
		t.Fatal("missing archive snapshot must not render navigation")
	}

	base["feed_archive"] = &pb.FeedArchiveStats{
		EntryCount: 75,
		Years: []*pb.FeedArchiveYear{
			{Year: 2026, EntryCount: 25},
			{Year: 2025, EntryCount: 50, Cursor: "year-boundary"},
		},
	}
	with := renderEmbeddedTemplate(t, "feed.html", base)
	for _, want := range []string{
		`class="menu feed-archive-menu"`,
		`href="/feed/archive">2026</a> <span class="feed-archive-count">25</span>`,
		`href="/feed/archive?cursor=year-boundary">2025</a> <span class="feed-archive-count">50</span>`,
	} {
		if !strings.Contains(with, want) {
			t.Fatalf("Feed archive sidebar missing %q", want)
		}
	}
	if strings.Contains(with, ">All</a>") {
		t.Fatal("Feed archive sidebar must not render an All navigation item")
	}
}

func TestAnonymousGroupDiscoveryIsReadableWithoutJavaScript(t *testing.T) {
	body := renderEmbeddedTemplate(t, "groups_public.html", pongo2.Context{
		"title": "Groups", "next_cursor": "next/page",
		"groups": []*pb.Profile{{Id: "book-club", Name: "读书会 & Friends", Description: "阅读 & 交流", Picture: "/book.png?a=1&b=2", Private: true}},
	})
	for _, want := range []string{
		`<h2 class="page-title">Groups</h2>`, `href="/feed/book-club"`, `>读书会 &amp; Friends</a>`,
		`src="/book.png?a=1&amp;b=2"`, `阅读 &amp; 交流`,
		`aria-label="Private"`, `href="/groups?cursor=next%2Fpage"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("anonymous Group SSR missing %q", want)
		}
	}
	if strings.Contains(body, `id="app-root"`) {
		t.Fatal("anonymous Group discovery must not depend on the React root")
	}
	if strings.Contains(body, "&amp;amp;") {
		t.Fatal("anonymous Group discovery must not double-escape profile fields")
	}
}
