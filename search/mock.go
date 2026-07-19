package search

import "github.com/blevesearch/bleve/v2"

func InitMockIndexService(indexPath string) {
	Indexer = NewMockIndex()
}

type embeddedIndex interface {
	bleve.Index
}

type MockIndex struct {
	embeddedIndex
}

var _ bleve.Index = (*MockIndex)(nil)

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

func (i MockIndex) Close() error {
	return nil
}

// Name returns the name of the index (by default this is the path)
func (i MockIndex) Name() string {
	return "MockIndex"
}
