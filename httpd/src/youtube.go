package server

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

func validYouTubeVideoID(id string) bool {
	if len(id) != 11 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func legacyYouTubeVideoIDFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "youtube.com" && host != "www.youtube.com" {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 || parts[0] != "v" {
		return ""
	}
	legacyID, _, _ := strings.Cut(parts[1], "&")
	id, err := url.PathUnescape(legacyID)
	if err != nil || !validYouTubeVideoID(id) {
		return ""
	}
	return id
}

// legacyYouTubeVideoID treats Thumbnail.player only as an extraction source.
// It accepts the exact YouTube /v/ URL shape used by archived FriendFeed
// object/embed players and never returns or renders their untrusted HTML.
func legacyYouTubeVideoID(player string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(player))
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			return ""
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		var candidate string
		switch token.Data {
		case "embed":
			for _, attr := range token.Attr {
				if attr.Key == "src" {
					candidate = attr.Val
				}
			}
		case "param":
			var name, value string
			for _, attr := range token.Attr {
				switch attr.Key {
				case "name":
					name = attr.Val
				case "value":
					value = attr.Val
				}
			}
			if strings.EqualFold(name, "movie") {
				candidate = value
			}
		}
		if id := legacyYouTubeVideoIDFromURL(candidate); id != "" {
			return id
		}
	}
}
