package search

import (
	"slices"
	"testing"
)

func TestTokenizerTerms(t *testing.T) {
	tokenizer := NewTokenizer()
	for _, test := range []struct {
		input string
		want  []string
	}{
		{input: "张无忌对张三丰说", want: []string{"张无忌", "对", "张三丰", "说"}},
		{input: "FriendFeed search works", want: []string{"friendfeed", " ", "search", " ", "works"}},
	} {
		stream := tokenizer.Tokenize([]byte(test.input))
		terms := make([]string, 0, len(stream))
		for _, token := range stream {
			terms = append(terms, string(token.Term))
		}
		if !slices.Equal(terms, test.want) {
			t.Errorf("Tokenize(%q) = %#v; want %#v", test.input, terms, test.want)
		}
	}
}
