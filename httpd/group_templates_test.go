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

	settings := renderEmbeddedTemplate(t, "group_settings.html", pongo2.Context{
		"title":        "Group settings",
		"group":        group,
		"error":        "boom",
		"form_action":  "/groups/book-club/settings",
		"submit_label": "Save",
		"cancel_url":   "/feed/book-club",
		"current_user": currentUser,
	})
	for _, want := range []string{
		`action="/groups/book-club/settings"`,
		`action="/groups/book-club/delete"`,
		`href="/groups/book-club/members"`,
		`value="Book Club"`,
		`class="error-banner" role="alert">boom`,
		`class="legacy-button danger"`,
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("group_settings.html missing %q", want)
		}
	}
	if strings.Contains(settings, "return confirm(") || strings.Contains(settings, "<style>") {
		t.Fatal("group_settings.html must not carry inline scripts or styles")
	}

	members := renderEmbeddedTemplate(t, "group_members.html", pongo2.Context{
		"title": "Group members",
		"group": group,
		"members": []*pb.GroupMember{
			{Profile: &pb.Profile{Uuid: "u1", Id: "alice", Name: "Alice"}, IsAdmin: true},
			{Profile: &pb.Profile{Uuid: "u2", Id: "bob", Name: "Bob"}},
		},
		"has_more":     true,
		"can_manage":   true,
		"current_user": currentUser,
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
		"title":        "Group members",
		"group":        group,
		"members":      []*pb.GroupMember{{Profile: &pb.Profile{Uuid: "u1", Id: "alice", Name: "Alice"}}},
		"current_user": currentUser,
	})
	if strings.Contains(plain, "members/action") {
		t.Fatal("plain member must not see management forms")
	}
}

func TestLayoutSidebarGroupsSection(t *testing.T) {
	// group_create.html extends layout.html, which exercises the sidebar.
	createCtx := func() pongo2.Context {
		return pongo2.Context{
			"title":        "Create Group",
			"form_action":  "/groups/create",
			"submit_label": "Create Group",
			"cancel_url":   "/",
			"show_id":      true,
			"show_private": true,
			"group":        &pb.Profile{},
			"current_user": &pb.Profile{Uuid: "u", Id: "me"},
			"user_groups": []*pb.Profile{
				{Id: "alpha", Name: "Alpha"},
				{Id: "secret-club", Name: "Secret Club", Private: true},
			},
		}
	}
	body := renderEmbeddedTemplate(t, "group_create.html", createCtx())
	for _, want := range []string{
		`<a href="/feed/alpha" title="alpha">Alpha</a>`,
		`Secret Club (private)`,
		// The create affordance is the "+" sharing the Groups heading row,
		// in its own block below the navigation menu.
		`<h3 class="groups-heading">Groups <a href="/groups/create" title="Create a group">+</a></h3>`,
		`<li><a href="/groups">Groups</a></li>`,
		`name="id"`,
		`name="picture"`,
		`name="private"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sidebar/create page missing %q", want)
		}
	}
	if strings.Contains(body, `name="private" disabled`) {
		t.Fatal("private checkbox must be enabled: the approval flow exists now")
	}
	if strings.Contains(body, "<style>") || strings.Contains(body, "<script>") {
		t.Fatal("group_create.html must not carry inline styles or scripts")
	}
	// The Groups block lives outside (below) the navigation <details>.
	if strings.Index(body, "</details>") > strings.Index(body, "groups-menu") {
		t.Fatal("Groups block must render below the navigation menu, not inside it")
	}

	// Logged in with no Groups: the block still renders the create affordance.
	empty := createCtx()
	empty["user_groups"] = []*pb.Profile{}
	emptyBody := renderEmbeddedTemplate(t, "group_create.html", empty)
	if !strings.Contains(emptyBody, `groups-menu`) || !strings.Contains(emptyBody, `title="Create a group">+</a>`) {
		t.Fatal("empty Groups block must still offer the create affordance")
	}

	// Anonymous visitors get no Groups block at all.
	anon := createCtx()
	delete(anon, "current_user")
	delete(anon, "user_groups")
	anonBody := renderEmbeddedTemplate(t, "group_create.html", anon)
	if strings.Contains(anonBody, "groups-menu") || strings.Contains(anonBody, "Create a group") {
		t.Fatal("anonymous render must not contain the Groups block")
	}
}

func TestGroupsPageRendersCompleteList(t *testing.T) {
	body := renderEmbeddedTemplate(t, "groups.html", pongo2.Context{
		"title":        "My groups",
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
		`class="admin-badge">private</span>`,
		`href="/groups/create"`,
		`<img class="avatar" src="https://example.com/a.png"`,
		`<img class="avatar" src="/static/images/ff-default.jpg"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("groups.html missing %q", want)
		}
	}
}
