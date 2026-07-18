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

func InitIndexService(indexPath string) {
	Indexer = NewIndex(indexPath)
}

func NewIndex(indexPath string) bleve.Index {
	if err := os.MkdirAll(indexPath, os.ModePerm); err != nil {
		panic(err)
	}

	// open the index
	idx, err := bleve.Open(indexPath)
	if errors.Is(err, bleve.ErrorIndexMetaMissing) {
		log.Println("Load mapping....")
		mapping := bleve.NewIndexMapping()
		err := mapping.AddCustomTokenizer("gse",
			map[string]any{
				"type":       "gse",
				"user_dicts": "./data/dict/zh/dict.txt",
			},
		)
		if err != nil {
			panic(err)
		}

		log.Println("custom analyer")
		err = mapping.AddCustomAnalyzer("gse",
			map[string]any{
				"type":      "gse",
				"tokenizer": "gse",
			},
		)
		if err != nil {
			panic(err)
		}
		mapping.DefaultAnalyzer = "gse"

		idx, err := bleve.New(indexPath, mapping)
		if err != nil {
			panic(err)
		}
		log.Println("index returned")
		return idx
	} else if err != nil {
		log.Fatal(err)
	} else {
		log.Printf("Opening existing index...")
	}
	return idx
}

// func Search() {
// 	index, _ := bleve.Open("example.bleve")
// 	query := bleve.NewQueryStringQuery("bleve")
// 	searchRequest := bleve.NewSearchRequest(query)
// 	searchResult, _ := index.Search(searchRequest)
// }

func NewConstructor(config map[string]any, cache *registry.Cache) (analysis.Tokenizer, error) {
	return NewTokenizer(), nil
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
