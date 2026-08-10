package util

import (
	"strings"
	"testing"
)

func TestUrlToLinkUsesMatchPositions(t *testing.T) {
	got := UrlToLink("repeat https://example.com/x repeat https://example.com/x.")
	want := `repeat <a href="https://example.com/x">https://example.com/x</a> repeat <a href="https://example.com/x">https://example.com/x</a>.`
	if got != want {
		t.Fatalf("UrlToLink() = %q, want %q", got, want)
	}
	if strings.Contains(got, "<a href=\"<a") {
		t.Fatalf("UrlToLink() produced nested links: %s", got)
	}
}

func TestUrlToLinkNormalizesWWWAndIgnoresFilenameLikeDomains(t *testing.T) {
	got := UrlToLink("www.example.com config.json main.go")
	want := `<a href="http://www.example.com">www.example.com</a> config.json main.go`
	if got != want {
		t.Fatalf("UrlToLink() = %q, want %q", got, want)
	}
}

func TestEntityToLinkUsesInternalTagRoute(t *testing.T) {
	got := EntityToLink("#golang 和 #中文")
	want := `<a href="/tag/golang">#golang</a> 和 <a href="/tag/%E4%B8%AD%E6%96%87">#中文</a>`
	if got != want {
		t.Fatalf("EntityToLink() = %q, want %q", got, want)
	}
}

func TestLinkifyLeavesMarkupAndExistingLinksUntouched(t *testing.T) {
	input := `<p data-note="#attr">See <a href="https://example.com/#inside">#inside</a> and #outside</p>`
	got := UrlToLink(EntityToLink(input))
	want := `<p data-note="#attr">See <a href="https://example.com/#inside">#inside</a> and <a href="/tag/outside">#outside</a></p>`
	if got != want {
		t.Fatalf("linkify = %q, want %q", got, want)
	}
}

func TestEntityToLinkDoesNotLinkURLFragment(t *testing.T) {
	input := `https://example.com/path#fragment #tag`
	got := UrlToLink(EntityToLink(input))
	want := `<a href="https://example.com/path#fragment">https://example.com/path#fragment</a> <a href="/tag/tag">#tag</a>`
	if got != want {
		t.Fatalf("linkify = %q, want %q", got, want)
	}
}

func TestEntityToLinkRejectsNumericHashtag(t *testing.T) {
	if got := EntityToLink("#123"); got != "#123" {
		t.Fatalf("EntityToLink() = %q, want unchanged numeric hashtag", got)
	}
}

func TestLinkifyEscapesPlainTextBeforeAddingLinks(t *testing.T) {
	input := DefaultSanitize(`<script>alert(1)</script> #safe`)
	got := EntityToLink(input)
	if strings.Contains(got, "<script") || !strings.Contains(got, `href="/tag/safe"`) {
		t.Fatalf("unsafe or missing link output: %s", got)
	}
}
