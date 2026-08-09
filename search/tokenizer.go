package search

import (
	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/go-ego/gse"
)

type Tokenizer struct {
	seg *gse.Segmenter
}

// NewTokenizer loads the gse segmenter with its default dictionary. It is
// the compatibility entry point of NewTokenizerE and panics on any error;
// new callers should use NewTokenizerE and decide how to handle the failure
// themselves.
func NewTokenizer() *Tokenizer {
	tokenizer, err := NewTokenizerE()
	if err != nil {
		panic(err)
	}
	return tokenizer
}

// NewTokenizerE loads the gse segmenter and returns any dictionary loading
// failure as an error instead of panicking. With no arguments the default
// dictionary bundled with the gse module is loaded; pass explicit dictionary
// file paths to override it (see gse.Segmenter.LoadDict).
func NewTokenizerE(dicts ...string) (*Tokenizer, error) {
	var seg gse.Segmenter
	// Loading the default dictionary
	if err := seg.LoadDict(dicts...); err != nil {
		return nil, err
	}
	return &Tokenizer{&seg}, nil
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
