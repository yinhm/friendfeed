package util

import (
	"strings"
	"testing"
)

func TestTruncateDoesNotCloseBalancedNestedTags(t *testing.T) {
	body := `<h1>title</h1><p>intro</p>` +
		`<div><div><img src="a.jpg"/></div></div>` +
		`<div><div><img src="b.jpg"/></div></div>` +
		`<p>` + strings.Repeat("长", 400)
	got := Truncate(body, 300, `<a href="/e/1">Read more...</a>`)
	if opens, closes := strings.Count(got, "<div>"), strings.Count(got, "</div>"); opens != closes {
		t.Fatalf("Truncate() left unbalanced divs: <div>=%d </div>=%d\n%s", opens, closes, got)
	}
	if !strings.HasSuffix(got, "Read more...</a></p>") {
		t.Fatalf("Truncate() should close only the open <p>, got tail: %s", got[len(got)-80:])
	}
}

func TestTruncateClosesOpenTagsInnermostFirst(t *testing.T) {
	body := `<div><p>` + strings.Repeat("a b ", 100) + `</p></div>`
	got := Truncate(body, 20, "...")
	if !strings.HasSuffix(got, "...</p></div>") {
		t.Fatalf("Truncate() should close <p> then <div>, got: %s", got)
	}
}

func TestTruncatePlainText(t *testing.T) {
	got := Truncate(strings.Repeat("x", 100), 10, "...")
	if got != strings.Repeat("x", 10)+"..." {
		t.Fatalf("Truncate() = %q", got)
	}
}

func TestTruncateShortTextUnchanged(t *testing.T) {
	body := `<p>short</p>`
	if got := Truncate(body, 300, "..."); got != body {
		t.Fatalf("Truncate() = %q, want %q", got, body)
	}
}
