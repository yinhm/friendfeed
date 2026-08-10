package util

// Entity matching is a reduced local adaptation of
// github.com/cupcake/text-entities-go. See TEXT_ENTITIES_LICENSE.

import (
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
)

var (
	ugcSanitizer = newUGCSanitizer()
	urlPattern   = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s<>]+`)
)

type textEntity struct {
	start int
	end   int
	kind  entityKind
}

type entityKind uint8

const (
	entityURL entityKind = iota
	entityHashtag
)

func newUGCSanitizer() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowDataURIImages()
	return p
}

// UrlToLink linkifies URLs in the text nodes of a sanitized HTML fragment.
// Existing links and markup are left untouched.
func UrlToLink(body string) string {
	return linkHTML(body, true, false)
}

// EntityToLink linkifies hashtags in the text nodes of a sanitized HTML
// fragment. Hashtags inside URLs and existing links are left untouched.
func EntityToLink(body string) string {
	return linkHTML(body, false, true)
}

func DefaultSanitize(body string) string {
	return ugcSanitizer.Sanitize(body)
}

func linkHTML(body string, linkURLs, linkHashtags bool) string {
	var out strings.Builder
	insideLink := false
	position := 0
	for position < len(body) {
		relativeTagStart := strings.IndexByte(body[position:], '<')
		if relativeTagStart < 0 {
			writeLinkedText(&out, body[position:], insideLink, linkURLs, linkHashtags)
			break
		}
		tagStart := position + relativeTagStart
		writeLinkedText(&out, body[position:tagStart], insideLink, linkURLs, linkHashtags)

		tagEnd := findTagEnd(body, tagStart)
		if tagEnd < 0 {
			writeLinkedText(&out, body[tagStart:], insideLink, linkURLs, linkHashtags)
			break
		}
		tag := body[tagStart : tagEnd+1]
		out.WriteString(tag)
		if name, closing := tagName(tag); name == "a" {
			insideLink = !closing
		}
		position = tagEnd + 1
	}
	return out.String()
}

func findTagEnd(body string, start int) int {
	var quote byte
	for i := start + 1; i < len(body); i++ {
		switch body[i] {
		case '\'', '"':
			if quote == 0 {
				quote = body[i]
			} else if quote == body[i] {
				quote = 0
			}
		case '>':
			if quote == 0 {
				return i
			}
		}
	}
	return -1
}

func tagName(tag string) (name string, closing bool) {
	i := 1
	for i < len(tag) && (tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\n' || tag[i] == '\r') {
		i++
	}
	if i < len(tag) && tag[i] == '/' {
		closing = true
		i++
	}
	start := i
	for i < len(tag) && ((tag[i] >= 'a' && tag[i] <= 'z') || (tag[i] >= 'A' && tag[i] <= 'Z') || (tag[i] >= '0' && tag[i] <= '9')) {
		i++
	}
	return strings.ToLower(tag[start:i]), closing
}

func writeLinkedText(out *strings.Builder, fragment string, insideLink, linkURLs, linkHashtags bool) {
	if fragment == "" || insideLink {
		out.WriteString(fragment)
		return
	}
	// Normalize linked fragments through one decode/encode pass so generated
	// attributes and existing text are escaped exactly once.
	text := html.UnescapeString(fragment)
	matches := extractTextEntities(text)

	position := 0
	wroteLink := false
	for _, match := range matches {
		if (match.kind == entityURL && !linkURLs) || (match.kind == entityHashtag && !linkHashtags) {
			continue
		}
		if position < match.start {
			out.WriteString(html.EscapeString(text[position:match.start]))
		}

		value := text[match.start:match.end]
		href := value
		if match.kind == entityHashtag {
			href = "/tag/" + url.PathEscape(strings.TrimPrefix(strings.TrimPrefix(value, "#"), "＃"))
		} else if strings.HasPrefix(strings.ToLower(value), "www.") {
			href = "http://" + value
		}
		out.WriteString(`<a href="`)
		out.WriteString(html.EscapeString(href))
		out.WriteString(`">`)
		out.WriteString(html.EscapeString(value))
		out.WriteString(`</a>`)
		position = match.end
		wroteLink = true
	}
	if !wroteLink {
		out.WriteString(fragment)
		return
	}
	if position < len(text) {
		out.WriteString(html.EscapeString(text[position:]))
	}
}

func extractTextEntities(text string) []textEntity {
	urls := extractURLs(text)
	hashtags := extractHashtags(text)
	entities := make([]textEntity, 0, len(urls)+len(hashtags))
	entities = append(entities, urls...)
	for _, hashtag := range hashtags {
		if !overlapsAny(hashtag, urls) {
			entities = append(entities, hashtag)
		}
	}
	sort.Slice(entities, func(i, j int) bool { return entities[i].start < entities[j].start })
	return entities
}

func extractURLs(text string) []textEntity {
	indices := urlPattern.FindAllStringIndex(text, -1)
	entities := make([]textEntity, 0, len(indices))
	for _, index := range indices {
		start, end := index[0], trimURL(text, index[0], index[1])
		if end <= start || invalidURLPrefix(text, start) {
			continue
		}
		entities = append(entities, textEntity{start: start, end: end, kind: entityURL})
	}
	return entities
}

func invalidURLPrefix(text string, start int) bool {
	if start == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text[:start])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("@_-", r)
}

func trimURL(text string, start, end int) int {
	for end > start {
		r, size := utf8.DecodeLastRuneInString(text[start:end])
		if strings.ContainsRune(".,!?;:'\"", r) || unmatchedClosing(text[start:end], r) {
			end -= size
			continue
		}
		break
	}
	return end
}

func unmatchedClosing(value string, closing rune) bool {
	var opening rune
	switch closing {
	case ')':
		opening = '('
	case ']':
		opening = '['
	case '}':
		opening = '{'
	default:
		return false
	}
	return strings.Count(value, string(closing)) > strings.Count(value, string(opening))
}

func extractHashtags(text string) []textEntity {
	var entities []textEntity
	for offset := 0; offset < len(text); {
		r, size := utf8.DecodeRuneInString(text[offset:])
		if r != '#' && r != '＃' {
			offset += size
			continue
		}
		if offset > 0 {
			before, _ := utf8.DecodeLastRuneInString(text[:offset])
			if hashtagRune(before) || before == '&' {
				offset += size
				continue
			}
		}

		end := offset + size
		hasNonDigit := false
		for end < len(text) {
			candidate, candidateSize := utf8.DecodeRuneInString(text[end:])
			if !hashtagRune(candidate) {
				break
			}
			if !unicode.IsDigit(candidate) {
				hasNonDigit = true
			}
			end += candidateSize
		}
		if end > offset+size && hasNonDigit && !invalidHashtagSuffix(text[end:]) {
			entities = append(entities, textEntity{start: offset, end: end, kind: entityHashtag})
			offset = end
			continue
		}
		offset += size
	}
	return entities
}

func hashtagRune(r rune) bool {
	return r == '_' || r == '\u200c' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

func invalidHashtagSuffix(value string) bool {
	return strings.HasPrefix(value, "#") || strings.HasPrefix(value, "＃") || strings.HasPrefix(value, "://")
}

func overlapsAny(entity textEntity, candidates []textEntity) bool {
	for _, candidate := range candidates {
		if entity.start < candidate.end && candidate.start < entity.end {
			return true
		}
	}
	return false
}
