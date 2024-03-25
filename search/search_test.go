package search

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/stretchr/testify/assert"
)

func TestSearch(t *testing.T) {
	dbpath := os.TempDir() + "/fftestdb"

	InitIndexService(filepath.Join(dbpath, "index"))

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

	Indexer.Close()

	os.RemoveAll(dbpath)
}
