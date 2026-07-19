package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/flosch/pongo2"
)

func newTestFriendRender(t *testing.T) *FriendRender {
	t.Helper()
	renderer, err := NewFriendRender(fstest.MapFS{
		"page.html": &fstest.MapFile{Data: []byte("Hello {{ name }}")},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	return renderer
}

func TestFriendRenderReturnsTemplateLoadError(t *testing.T) {
	renderer := newTestFriendRender(t)
	response := httptest.NewRecorder()

	err := renderer.Instance("missing.html", pongo2.Context{}).Render(response)
	if err == nil || !strings.Contains(err.Error(), "load template") {
		t.Fatalf("expected template load error, got %v", err)
	}
}

func TestFriendRenderReturnsInvalidContextError(t *testing.T) {
	renderer := newTestFriendRender(t)
	response := httptest.NewRecorder()

	err := renderer.Instance("page.html", map[string]any{"name": "FriendFeed"}).Render(response)
	if err == nil || !strings.Contains(err.Error(), "expected pongo2.Context") {
		t.Fatalf("expected invalid context error, got %v", err)
	}
}

func TestFriendRenderExecutesTemplate(t *testing.T) {
	renderer := newTestFriendRender(t)
	response := httptest.NewRecorder()

	err := renderer.Instance("page.html", pongo2.Context{"name": "FriendFeed"}).Render(response)
	if err != nil {
		t.Fatal(err)
	}
	if response.Body.String() != "Hello FriendFeed" {
		t.Fatalf("unexpected response body %q", response.Body.String())
	}
}
