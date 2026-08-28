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

func TestGroupTemplatesCompileAndRender(t *testing.T) {
	group := &pb.Profile{
		Uuid:        "22222222-2222-2222-2222-222222222222",
		Id:          "book-club",
		Name:        "Book Club",
		Description: "reading",
		Picture:     "https://example.com/p.png",
	}
	currentUser := &pb.Profile{Uuid: "11111111-1111-1111-1111-111111111111", Id: "me"}

	members := renderEmbeddedTemplate(t, "group_members.html", pongo2.Context{
		"title": "Group members",
		"group": group,
		"members": []*pb.GroupMember{
			{Profile: &pb.Profile{Uuid: "u1", Id: "alice", Name: "Alice"}, IsAdmin: true},
			{Profile: &pb.Profile{Uuid: "u2", Id: "bob", Name: "Bob"}},
		},
		"has_more":             true,
		"can_manage":           true,
		"feed_management_id":   "book-club",
		"feed_management_page": "members",
		"group_settings_url":   "/groups/book-club/settings",
		"group_members_url":    "/groups/book-club/members",
		"manage_services_url":  "/feed/book-club/import",
		"current_user":         currentUser,
	})
	for _, want := range []string{
		`action="/groups/book-club/members/action"`,
		`value="demote"`,
		`value="promote"`,
		`value="remove"`,
		"more members",
		`href="/groups/book-club/settings"`,
	} {
		if !strings.Contains(members, want) {
			t.Fatalf("group_members.html missing %q", want)
		}
	}

	// Members page for a plain logged-in user hides management controls.
	plain := renderEmbeddedTemplate(t, "group_members.html", pongo2.Context{
		"title":                "Group members",
		"group":                group,
		"members":              []*pb.GroupMember{{Profile: &pb.Profile{Uuid: "u1", Id: "alice", Name: "Alice"}}},
		"feed_management_id":   "book-club",
		"feed_management_page": "members",
		"group_members_url":    "/groups/book-club/members",
		"current_user":         currentUser,
	})
	if strings.Contains(plain, "members/action") {
		t.Fatal("plain member must not see management forms")
	}
	if strings.Contains(plain, ">Settings</a>") || strings.Contains(plain, ">Import Services</a>") {
		t.Fatal("plain member must not see admin navigation")
	}
	if !strings.Contains(plain, `aria-current="page" class="border-b-2 border-primary px-3 py-2 text-sm font-medium text-primary">Members</a>`) {
		t.Fatal("members navigation must remain available and selected")
	}
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

func TestLayoutSidebarGroupsSection(t *testing.T) {
	// app_shell.html extends layout.html, which exercises the sidebar used by
	// authenticated React pages. The Group form has a component test.
	createCtx := func() pongo2.Context {
		return pongo2.Context{
			"title":               "Create Group",
			"form_action":         "/groups/create",
			"submit_label":        "Create Group",
			"cancel_url":          "/",
			"show_id":             true,
			"show_private":        true,
			"group_page":          "create",
			"group":               &pb.Profile{},
			"current_user":        &pb.Profile{Uuid: "u", Id: "me"},
			"show_groups_sidebar": true,
			"user_groups": []*pb.Profile{
				{Id: "alpha", Name: "Alpha"},
				{Id: "secret-club", Name: "Secret Club", Private: true},
			},
		}
	}
	body := renderEmbeddedTemplate(t, "app_shell.html", createCtx())
	for _, want := range []string{
		`<a href="/feed/alpha" title="alpha">Alpha</a>`,
		`<a href="/feed/secret-club" title="secret-club">Secret Club</a><span class="private-icon" role="img" aria-label="Private" title="Private"></span>`,
		`<h3 class="groups-heading">Groups</h3>`,
		`<li><a href="/groups">Groups</a></li>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sidebar/create page missing %q", want)
		}
	}
	// The Groups block lives outside (below) the navigation <details>.
	if strings.Index(body, "</details>") > strings.Index(body, "groups-menu") {
		t.Fatal("Groups block must render below the navigation menu, not inside it")
	}

	// Ordinary pages omit the Home-only Groups sidebar even when group data is
	// accidentally present in their render context.
	nonHome := createCtx()
	delete(nonHome, "show_groups_sidebar")
	nonHomeBody := renderEmbeddedTemplate(t, "app_shell.html", nonHome)
	if strings.Contains(nonHomeBody, "groups-menu") {
		t.Fatal("non-Home page must not render the Groups block")
	}

	// Logged in with no Groups: the block still links to the full list.
	empty := createCtx()
	empty["user_groups"] = []*pb.Profile{}
	emptyBody := renderEmbeddedTemplate(t, "app_shell.html", empty)
	if !strings.Contains(emptyBody, `groups-menu`) || !strings.Contains(emptyBody, `>More&hellip;</a>`) {
		t.Fatal("empty Groups block must still link to the full list")
	}

	// Anonymous visitors get no Groups block at all.
	anon := createCtx()
	delete(anon, "current_user")
	delete(anon, "user_groups")
	anonBody := renderEmbeddedTemplate(t, "app_shell.html", anon)
	if strings.Contains(anonBody, "groups-menu") || strings.Contains(anonBody, "Create a group") {
		t.Fatal("anonymous render must not contain the Groups block")
	}
}

func TestGroupsPageRendersCompleteList(t *testing.T) {
	body := renderEmbeddedTemplate(t, "groups.html", pongo2.Context{
		"title":        "My groups",
		"heading":      "My groups",
		"show_create":  true,
		"empty_text":   "You have not joined any groups yet.",
		"current_user": &pb.Profile{Uuid: "u", Id: "me"},
		"groups": []*pb.Profile{
			{Id: "alpha", Name: "Alpha", Description: "First", Picture: "https://example.com/a.png"},
			// Handlers substitute the fixed fallback avatar for empty
			// pictures before rendering; the template always renders one.
			{Id: "secret", Name: "Secret", Private: true, Picture: "/static/images/ff-default.jpg"},
		},
	})
	for _, want := range []string{
		`<h2 class="page-title">My groups</h2>`,
		`href="/feed/alpha"`,
		`href="/feed/secret"`,
		`href="/feed/secret" title="secret">Secret</a><span class="private-icon" role="img" aria-label="Private" title="Private"></span>`,
		`href="/groups/create"`,
		`<img class="avatar" src="https://example.com/a.png"`,
		`<img class="avatar" src="/static/images/ff-default.jpg"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("groups.html missing %q", want)
		}
	}
	if strings.Contains(body, `>private</span>`) || strings.Contains(body, `(private)`) {
		t.Fatal("private Groups must use the lock icon instead of a text label")
	}
}
