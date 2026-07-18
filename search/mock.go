package search

import (
	"context"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	index "github.com/blevesearch/bleve_index_api"
)

func InitMockIndexService(indexPath string) {
	Indexer = NewMockIndex()
}

type MockIndex struct {
}

func NewMockIndex() *MockIndex {
	return &MockIndex{}
}

// Index analyzes, indexes or stores mapped data fields. Supplied
// identifier is bound to analyzed data and will be retrieved by search
// requests. See Index interface documentation for details about mapping
// rules.
func (i MockIndex) Index(id string, data any) error {
	return nil
}

func (i MockIndex) Delete(id string) error {
	return nil
}

func (i MockIndex) NewBatch() *bleve.Batch {
	return nil
}

func (i MockIndex) Batch(b *bleve.Batch) error {
	return nil
}

// Document returns specified document or nil if the document is not
// indexed or stored.
func (i MockIndex) Document(id string) (index.Document, error) {
	return nil, nil
}

// DocCount returns the number of documents in the index.
func (i MockIndex) DocCount() (uint64, error) {
	return 0, nil
}

func (i MockIndex) Search(req *bleve.SearchRequest) (*bleve.SearchResult, error) {
	return nil, nil
}
func (i MockIndex) SearchInContext(ctx context.Context, req *bleve.SearchRequest) (*bleve.SearchResult, error) {
	return nil, nil
}

func (i MockIndex) Fields() (a []string, b error) {
	return
}

func (i MockIndex) FieldDict(field string) (a index.FieldDict, b error) {
	return
}

func (i MockIndex) FieldDictRange(field string, startTerm []byte, endTerm []byte) (a index.FieldDict, b error) {
	return
}

func (i MockIndex) FieldDictPrefix(field string, termPrefix []byte) (a index.FieldDict, b error) {
	return
}

func (i MockIndex) Close() error {
	return nil
}

func (i MockIndex) Mapping() mapping.IndexMapping {
	return nil
}

func (i MockIndex) Stats() *bleve.IndexStat {
	return nil
}
func (i MockIndex) StatsMap() (a map[string]any) {
	return
}

func (i MockIndex) GetInternal(key []byte) (a []byte, b error) {
	return
}
func (i MockIndex) SetInternal(key, val []byte) error {
	return nil
}
func (i MockIndex) DeleteInternal(key []byte) error {
	return nil
}

// Name returns the name of the index (by default this is the path)
func (i MockIndex) Name() string {
	return "MockIndex"
}

// SetName lets you assign your own logical name to this index
func (i MockIndex) SetName(string) {
}

// Advanced returns the internal index implementation
func (i MockIndex) Advanced() (a index.Index, b error) {
	return
}
