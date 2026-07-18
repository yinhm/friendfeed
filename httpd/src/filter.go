package server

import (
	"github.com/microcosm-cc/bluemonday"
)

var htmlSanitizer *bluemonday.Policy

func init() {
	htmlSanitizer = bluemonday.StripTagsPolicy()
	htmlSanitizer.AllowElements("h1", "h2", "h3", "h4")

	// The following are all inline phrasing elements
	htmlSanitizer.AllowElements("cite", "code", "em", "mark", "strong")
	// block and inline elements that impart no semantic meaning but style the
	// document
	htmlSanitizer.AllowElements("b", "i", "pre", "small", "strike")

	// "br" "p" "span" are permitted and take no attributes
	htmlSanitizer.AllowElements("br", "p", "span")
}
