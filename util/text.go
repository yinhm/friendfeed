package util

import (
	"fmt"
	"strings"

	text "github.com/cupcake/text-entities-go"
	"github.com/microcosm-cc/bluemonday"
)

var ugcSanitizer *bluemonday.Policy

func init() {
	ugcSanitizer = bluemonday.UGCPolicy()
}

func EntityToLink(body string) string {
	urls := text.ExtractURLs(body)
	for _, url := range urls {
		new := fmt.Sprintf("<a href=\"%s\">%s</a>", url, url)
		body = strings.Replace(body, url, new, -1)
	}
	tags := text.ExtractHashtags(body)
	for _, tag := range tags {
		new := fmt.Sprintf("<a href=\"/hashtag/%s\">%s</a>", tag, tag)
		body = strings.Replace(body, tag, new, -1)
	}
	return body
}

func DefaultSanitize(body string) string {
	return ugcSanitizer.Sanitize(body)
}
