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
		"current_user": currentUser,
	})
	for _, want := range []string{
		`action="/groups/book-club/settings"`,
		`action="/groups/book-club/delete"`,
		`href="/groups/book-club/members"`,
		"return confirm(",
		`value="Book Club"`,
		"boom",
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("group_settings.html missing %q", want)
		}
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
	body := renderEmbeddedTemplate(t, "group_create.html", pongo2.Context{
		"title":        "Create Group",
		"current_user": &pb.Profile{Uuid: "u", Id: "me"},
		"user_groups": []*pb.Profile{
			{Id: "alpha", Name: "Alpha"},
			{Id: "secret-club", Name: "Secret Club", Private: true},
		},
	})
	for _, want := range []string{
		`<a href="/feed/alpha" title="alpha">Alpha</a>`,
		`Secret Club (private)`,
		`/groups/create">Create a group`,
		`name="id"`,
		`name="picture"`,
		`name="private" disabled`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("sidebar/create page missing %q", want)
		}
	}

	// Logged in with no Groups: the section still renders the create link.
	empty := renderEmbeddedTemplate(t, "group_create.html", pongo2.Context{
		"title":        "Create Group",
		"current_user": &pb.Profile{Uuid: "u", Id: "me"},
		"user_groups":  []*pb.Profile{},
	})
	if !strings.Contains(empty, `/groups/create">Create a group`) {
		t.Fatal("empty Groups section must still offer the create link")
	}

	// Anonymous visitors get no Groups section at all.
	anon := renderEmbeddedTemplate(t, "group_create.html", pongo2.Context{"title": "Create Group"})
	if strings.Contains(anon, "Create a group") || strings.Contains(anon, "<h3>Groups</h3>") {
		t.Fatal("anonymous render must not contain the Groups section")
	}
}
