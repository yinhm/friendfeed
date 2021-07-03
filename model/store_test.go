package store

import (
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type DbTestSuite struct {
	suite.Suite

	db *Store
}

func TestDbTestSuite(t *testing.T) {
	suite.Run(t, new(DbTestSuite))
}

func (s *DbTestSuite) SetupSuite() {
	dbpath := os.TempDir() + "/testmcsdb"
	s.db = NewStore(dbpath)
}

func (s *DbTestSuite) TearDownSuite() {
	s.db.Close()
	err := os.RemoveAll(s.db.path)
	if err != nil {
		log.Println("can not remove test db.")
	}
}

func (s *DbTestSuite) TestDbGetPut() {
	err := s.db.Set([]byte("key1"), []byte("value1"))
	assert.Nil(s.T(), err)

	value, err := s.db.Get([]byte("key1"))
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), string(value), "value1")

	value, err = s.db.Get([]byte("key2"))
	assert.NotNil(s.T(), err)
	assert.Nil(s.T(), value)

	err = s.db.Delete([]byte("key1"))
	assert.Nil(s.T(), err)

	value, err = s.db.Get([]byte("key1"))
	assert.NotNil(s.T(), err)
	assert.Nil(s.T(), value)
}
