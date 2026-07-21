// Copyright 2016 The Hugo Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	tagRE        = regexp.MustCompile(`^<(/)?([^ ]+?)(?:(\s*/)| .*?)?>`)
	htmlSinglets = map[string]bool{
		"br": true, "col": true, "link": true,
		"base": true, "img": true, "param": true,
		"area": true, "hr": true, "input": true,
	}
)

type htmlTag struct {
	name    string
	pos     int
	openTag bool
}

// Truncate truncates a given string to the specified length.
func Truncate(text string, length int, ellipsis string) string {
	// ellipsis = html.EscapeString(ellipsis)

	if utf8.RuneCountInString(text) <= length {
		return text
	}

	tags := []htmlTag{}
	var lastWordIndex, lastNonSpace, currentLen, endTextPos, nextTag int

	for i, r := range text {
		if i < nextTag {
			continue
		}

		// Make sure we keep tag of HTML tags
		slice := text[i:]
		m := tagRE.FindStringSubmatchIndex(slice)
		if len(m) > 0 && m[0] == 0 {
			nextTag = i + m[1]
			tagname := slice[m[4]:m[5]]
			lastWordIndex = lastNonSpace
			_, singlet := htmlSinglets[tagname]
			if !singlet && m[6] == -1 {
				tags = append(tags, htmlTag{name: tagname, pos: i, openTag: m[2] == -1})
			}

			continue
		}

		currentLen++
		if unicode.IsSpace(r) {
			lastWordIndex = lastNonSpace
		} else if unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana) {
			lastWordIndex = i
		} else {
			lastNonSpace = i + utf8.RuneLen(r)
		}

		if currentLen > length {
			if lastWordIndex == 0 {
				endTextPos = i
			} else {
				endTextPos = lastWordIndex
			}
			var out strings.Builder
			out.WriteString(text[0:endTextPos])
			out.WriteString(ellipsis)
			// Close out any open HTML tags
			var currentTag *htmlTag
			for _, tag := range slices.Backward(tags) {
				if tag.pos >= endTextPos || currentTag != nil {
					if currentTag != nil && currentTag.name == tag.name {
						currentTag = nil
					}
					continue
				}

				if tag.openTag {
					out.WriteString(("</" + tag.name + ">"))
				} else {
					currentTag = &tag
				}
			}

			return out.String()
		}
	}

	return text
}
