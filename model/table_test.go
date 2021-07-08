package model

import (
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
)

type TableTestSuite struct {
	suite.Suite

	db *store.Store
}

func TestTableTestSuite(t *testing.T) {
	suite.Run(t, new(TableTestSuite))
}

func (s *TableTestSuite) SetupSuite() {
	dbpath := os.TempDir() + "/testmcsdb"
	s.db = store.NewStore(dbpath)

	// InitTables(s.db)
}

func (s *TableTestSuite) TearDownSuite() {
	s.db.Close()
	err := s.db.Destroy()
	if err != nil {
		log.Println("can not remove test db.")
	}
}

func (s *TableTestSuite) TestTableFarm() {
	key := store.KeyFromString(Stock.NewKey("000001"))
	assert.Equal(s.T(), 16, len(key))

	/// 000000657398ab337c5642fbbcb46f85bae90436
	farmHash := "eab3360472a0425ab5f214afc8ed5d7a"
	assert.Equal(s.T(), 16, len(store.KeyFromString(farmHash)))

	p := &pb.Feed{
		Name: "000001",
	}

	err := Stock.Put(s.db, farmHash, p)
	assert.Nil(s.T(), err)

	farm := new(pb.Feed)
	err = Stock.Get(s.db, farmHash, farm)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "000001", farm.Name)
}
