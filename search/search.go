package search

import (
	"errors"
	"log"
	"os"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/registry"
)

var Indexer bleve.Index

func init() {
	registry.RegisterTokenizer("gse", NewConstructor)
	registry.RegisterAnalyzer("gse", NewAnalyzer)
}

// InitIndexService opens (or creates) the index at indexPath and stores it
// in the global Indexer. It is the compatibility entry point of
// InitIndexServiceE and panics on any error; new callers should use
// InitIndexServiceE and decide how to handle the failure themselves.
func InitIndexService(indexPath string) {
	if err := InitIndexServiceE(indexPath); err != nil {
		panic(err)
	}
}

// InitIndexServiceE opens (or creates) the index at indexPath and stores it
// in the global Indexer. The global Indexer is only assigned on success; on
// failure an error is returned and the global state is left untouched.
func InitIndexServiceE(indexPath string) error {
	idx, err := OpenIndex(indexPath)
	if err != nil {
		return err
	}
	Indexer = idx
	return nil
}

// CloseIndexService closes the global Indexer if it was successfully
// initialized and clears it afterwards. It is safe to call when Indexer is
// nil and safe to call multiple times: bleve Close is invoked at most once
// per initialized index.
func CloseIndexService() error {
	if Indexer == nil {
		return nil
	}
	err := Indexer.Close()
	Indexer = nil
	return err
}

// NewIndex opens (or creates) the index at indexPath. It is the
// compatibility entry point of OpenIndex and panics on any error; new
// callers should use OpenIndex and decide how to handle the failure
// themselves.
func NewIndex(indexPath string) bleve.Index {
	idx, err := OpenIndex(indexPath)
	if err != nil {
		panic(err)
	}
	return idx
}

// OpenIndex opens the existing index at indexPath, or creates a new one with
// the gse mapping when the index metadata is missing. All failures are
// returned as errors; it never panics and never calls log.Fatal.
func OpenIndex(indexPath string) (bleve.Index, error) {
	if err := os.MkdirAll(indexPath, os.ModePerm); err != nil {
		return nil, err
	}

	// open the index
	idx, err := bleve.Open(indexPath)
	if err == nil {
		log.Printf("Opening existing index...")
		return idx, nil
	}
	if !errors.Is(err, bleve.ErrorIndexMetaMissing) {
		return nil, err
	}

	log.Println("Load mapping....")
	mapping := bleve.NewIndexMapping()
	err = mapping.AddCustomTokenizer("gse",
		map[string]any{
			"type":       "gse",
			"user_dicts": "./data/dict/zh/dict.txt",
		},
	)
	if err != nil {
		return nil, err
	}

	log.Println("custom analyer")
	err = mapping.AddCustomAnalyzer("gse",
		map[string]any{
			"type":      "gse",
			"tokenizer": "gse",
		},
	)
	if err != nil {
		return nil, err
	}
	mapping.DefaultAnalyzer = "gse"

	idx, err = bleve.New(indexPath, mapping)
	if err != nil {
		return nil, err
	}
	log.Println("index returned")
	return idx, nil
}

// func Search() {
// 	index, _ := bleve.Open("example.bleve")
// 	query := bleve.NewQueryStringQuery("bleve")
// 	searchRequest := bleve.NewSearchRequest(query)
// 	searchResult, _ := index.Search(searchRequest)
// }

func NewConstructor(config map[string]any, cache *registry.Cache) (analysis.Tokenizer, error) {
	tokenizer, err := NewTokenizerE()
	if err != nil {
		return nil, err
	}
	return tokenizer, nil
}

func NewAnalyzer(config map[string]any, cache *registry.Cache) (analysis.Analyzer, error) {
	tokenizerName, ok := config["tokenizer"].(string)
	if !ok {
		return nil, errors.New("must specify tokenizer")
	}
	tokenizer, err := cache.TokenizerNamed(tokenizerName)
	if err != nil {
		return nil, err
	}
	analyzer := &analysis.DefaultAnalyzer{Tokenizer: tokenizer}
	return analyzer, nil
}
