package search

import (
	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/go-ego/gse"
)

type Tokenizer struct {
	seg *gse.Segmenter
}

func NewTokenizer() *Tokenizer {
	var seg gse.Segmenter
	// Loading the default dictionary
	err := seg.LoadDict()
	if err != nil {
		panic(err)
	}
	return &Tokenizer{&seg}
}

func (t *Tokenizer) Tokenize(textByte []byte) analysis.TokenStream {
	result := make(analysis.TokenStream, 0)
	pos := 1
	segments := t.seg.Segment(textByte)
	for _, seg := range segments {
		token := analysis.Token{
			Term:     []byte(seg.Token().Text()),
			Start:    seg.Start(),
			End:      seg.End(),
			Position: pos,
			Type:     analysis.Ideographic,
		}
		result = append(result, &token)
		pos++
	}
	return result
}
