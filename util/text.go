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
	ugcSanitizer.AllowDataURIImages()
}

func UrlToLink(body string) string {
	urls := text.ExtractURLs(body)
	for _, url := range urls {
		new := fmt.Sprintf("<a href=\"%s\">%s</a>", url, url)
		body = strings.Replace(body, url, new, -1)
	}
	return body
}

func EntityToLink(body string) string {
	tags := text.ExtractHashtags(body)
	for _, tag := range tags {
		new := fmt.Sprintf("<a href=\"/tag/%s\">%s</a>", tag, tag)
		body = strings.Replace(body, tag, new, -1)
	}
	return body
}

func DefaultSanitize(body string) string {
	return ugcSanitizer.Sanitize(body)
}
