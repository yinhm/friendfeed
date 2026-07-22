package search

import (
	"log"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/stretchr/testify/assert"
)

func TestSearch(t *testing.T) {
	dbpath := t.TempDir()
	indexPath := filepath.Join(dbpath, "index")

	InitIndexService(indexPath)

	id := "2b43a9066074d120ed2e45494eea1797"
	body := "张无忌对张三丰说：“太师父，武当山的生活太寂寞了，只有清风和明月两个朋友能陪我玩。”"
	Indexer.Index(id, body)

	bReq := bleve.NewSearchRequest(bleve.NewQueryStringQuery("张三丰"))
	bReq.Highlight = bleve.NewHighlight()
	res, err := Indexer.Search(bReq)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(res.Hits))

	id = "random-some-id"
	Indexer.Index(id, body)
	res, err = Indexer.Search(bReq)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(res.Hits))

	for _, hit := range res.Hits {
		log.Printf("%s, (%f)\n", hit.ID, hit.Score)
	}

	assert.NoError(t, Indexer.Close())

	Indexer = NewIndex(indexPath)
	t.Cleanup(func() { assert.NoError(t, Indexer.Close()) })
	res, err = Indexer.Search(bReq)
	assert.NoError(t, err)
	assert.Len(t, res.Hits, 2)
}
