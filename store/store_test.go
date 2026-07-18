package store

import (
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type DBTestSuite struct {
	suite.Suite

	rdb    *Store
	mdb    *Store
	dbpath string
}

func TestDBTestSuite(t *testing.T) {
	suite.Run(t, new(DBTestSuite))
}

func (s *DBTestSuite) SetupTest() {
	log.Println("setup tests...")
	dbpath := os.TempDir() + "/testffdb2"
	s.rdb = NewStore(dbpath)
	// s.mdb = NewMetaStore(path.Join(dbpath, "meta"))
	s.mdb = s.rdb
}

func (s *DBTestSuite) TearDownTest() {
	log.Println("teardown tests...")
	// s.mdb.Close()
	// err := os.RemoveAll(s.mdb.dbpath)
	// if err != nil {
	// 	log.Println("can not remove test db.")
	// }

	s.rdb.Close()
	err := os.RemoveAll(s.rdb.dbpath)
	if err != nil {
		log.Println("can not remove test db.")
	}
}

func (s *DBTestSuite) TestDBGetPut() {
	err := s.rdb.Put([]byte("key1"), []byte("value1"))
	assert.Nil(s.T(), err)

	value, err := s.rdb.Get([]byte("key1"))
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "value1", string(value))

	value, err = s.rdb.Get([]byte("key2"))
	assert.Nil(s.T(), err)
}

func (s *DBTestSuite) TestSetSyncConcurrentWrites() {
	const writes = 100
	errs := make(chan error, writes)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < writes; i++ {
			s.rdb.SetSync(i%2 == 0)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < writes; i++ {
			key := []byte(fmt.Sprintf("concurrent-key-%d", i))
			if err := s.rdb.Put(key, []byte("value")); err != nil {
				errs <- err
			}
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(s.T(), err)
	}
}

func (s *DBTestSuite) TestMetaStore() {
	// Giving meta store
	err := s.mdb.Put([]byte("key1"), []byte("value1"))
	assert.Nil(s.T(), err)

	value, err := s.mdb.Get([]byte("key1"))
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "value1", string(value))

	// With large key
	key := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdegfhijklmnopqrstuvwxyz")
	err = s.mdb.Put(key, []byte("value2"))
	assert.Nil(s.T(), err)

	value, err = s.mdb.Get(key)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "value2", string(value))
}

func (s *DBTestSuite) TestIteration() {
	// Giving meta store, When iterator data, it should find all keys
	prefix := []byte("job:feed:")
	key1 := fmt.Sprintf("job:feed:%s", "key1")
	key2 := fmt.Sprintf("job:feed:%s", "key2")

	err := s.mdb.Put([]byte(key1), []byte("value1"))
	assert.Nil(s.T(), err)

	err = s.mdb.Put([]byte(key2), []byte("value2"))
	assert.Nil(s.T(), err)

	opts := PrefixIteratorOptions(prefix)
	iter := newIterator(s.mdb.rdb, opts)
	defer iter.Close()

	iter.First()
	assert.True(s.T(), iter.Valid())
	assert.Equal(s.T(), key1, string(iter.Key()))
	assert.Equal(s.T(), "value1", string(iter.Value()))

	iter.Next()
	assert.True(s.T(), iter.Valid())
	assert.Equal(s.T(), key2, string(iter.Key()))
	assert.Equal(s.T(), "value2", string(iter.Value()))

	iter.Next()
	assert.False(s.T(), iter.Valid())

	assert.Nil(s.T(), iter.Error())
}

func (s *DBTestSuite) TestIterationReopen() {
	// Giving meta store, when iterator data, it should find all keys
	// first iter
	for i := 0; i < 3; i++ {
		key := NewFlakeKey(TableJobFeed, s.mdb.NextId())
		s.mdb.Put(key.Bytes(), []byte("value1"))
	}

	for i := 0; i < 2; i++ {
		key := NewFlakeKey(TableJobRunning, s.mdb.NextId())
		s.mdb.Put(key.Bytes(), []byte("value2"))
	}

	key := NewFlakeKey(TableMax, s.mdb.NextId())
	s.mdb.Put(key.Bytes(), []byte("value3"))

	key = NewFlakeKey(TableJobFeed, s.mdb.NextId())
	it := s.mdb.Iterator()
	it.SeekGE(key.Prefix().Bytes())
	numFound := 0
	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	// mdb switched to Block-based format
	assert.Equal(s.T(), 6, numFound)
	it.Close()

	// demonstrate data inconsistent when reopen db
	s.rdb.Close()
	// s.mdb.Close()
	s.SetupTest()

	// iter to key>=prefix
	key = NewFlakeKey(TableJobFeed, s.mdb.NextId())
	it = s.mdb.Iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())
	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 6, numFound)
	it.Close()

	// inconsistent occur
	key = NewFlakeKey(TableJobFeed, s.mdb.NextId())
	it = s.mdb.Iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())
	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	// was true for rocksdb
	// assert.Equal(s.T(), 3, numFound)
	assert.Equal(s.T(), 6, numFound)
	it.Close()

	// iter to key>=prefix
	key = NewFlakeKey(TableJobRunning, s.mdb.NextId())
	it = s.mdb.Iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())

	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 3, numFound)
	it.Close()

	// inconsistent occur
	key = NewFlakeKey(TableJobRunning, s.mdb.NextId())
	it = s.mdb.Iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())
	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	// was true for rocksdb
	// assert.Equal(s.T(), 2, numFound)
	assert.Equal(s.T(), 3, numFound)
	it.Close()
}

func (s *DBTestSuite) TestRockStorePrefixSeek() {
	// Giving meta store
	// First iteration: populate data
	batch := s.mdb.rdb.NewBatch()
	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableJobFeed, s.mdb.NextId())
		batch.Set(key.Bytes(), []byte("value1"), pebble.NoSync)
	}

	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableJobRunning, s.mdb.NextId())
		batch.Set(key.Bytes(), []byte("value2"), pebble.NoSync)
	}

	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableMax, s.mdb.NextId())
		batch.Set(key.Bytes(), []byte("value3"), pebble.NoSync)
	}
	batch.Commit(pebble.Sync)
	batch.Close()

	key := NewFlakeKey(TableJobFeed, s.mdb.NextId())
	it := s.mdb.Iterator()
	it.SeekGE(key.Prefix().Bytes())
	numFound := 0
	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 3000, numFound)
	it.Close()

	// Second iteration: reopen db
	s.rdb.Close()
	// s.mdb.Close()
	s.SetupTest()

	// iter to key>=prefix
	key = NewFlakeKey(TableJobFeed, s.mdb.NextId())
	it = s.mdb.Iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())

	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 3000, numFound)
	it.Close()

	key = NewFlakeKey(TableJobFeed, s.mdb.NextId())
	it = s.mdb.Iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())

	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 3000, numFound)
	it.Close()

	// iter to key>=prefix
	key = NewFlakeKey(TableJobRunning, s.mdb.NextId())
	it = s.mdb.Iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())

	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 2000, numFound)
	it.Close()

	key = NewFlakeKey(TableJobRunning, s.mdb.NextId())
	it = s.mdb.Iterator()
	defer it.Close()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())

	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 2000, numFound)
}

func (s *DBTestSuite) TestPrefixSeekWithDelimiterKey() {
	// Giving meta store
	//First iteration: populate data
	// @rdallman suggest this hack on gorocksdb issue #24
	// maxKey := []byte{
	// 	0xFF, 0xFF, 0xFF, 0xFF,
	// 	0xFF, 0xFF, 0xFF, 0xFF,
	// 	0xFF, 0xFF, 0xFF, 0xFF,
	// 	0xFF, 0xFF, 0xFF, 0xFF,
	// }
	// s.mdb.Put(maxKey, []byte(""))
	batch := s.mdb.rdb.NewBatch()
	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableJobFeed, s.mdb.NextId())
		batch.Set(key.Bytes(), []byte("value1"), pebble.NoSync)
	}

	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableJobRunning, s.mdb.NextId())
		batch.Set(key.Bytes(), []byte("value2"), pebble.NoSync)
	}

	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableMax, s.mdb.NextId())
		batch.Set(key.Bytes(), []byte("value3"), pebble.NoSync)
	}
	batch.Commit(pebble.Sync)
	batch.Close()

	key := NewFlakeKey(TableJobFeed, s.mdb.NextId())
	it := s.mdb.Iterator()
	it.SeekGE(key.Prefix().Bytes())

	numFound := 0
	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 3000, numFound)
	it.Close()

	// Second iteration: reopen db
	// reopen
	s.rdb.Close()
	// s.mdb.Close()
	s.SetupTest()

	// iter to key>=prefix
	key = NewFlakeKey(TableJobFeed, s.mdb.NextId())
	it = s.mdb.Iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())

	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 3000, numFound)
	it.Close()

	// again
	key = NewFlakeKey(TableJobFeed, s.mdb.NextId())
	it = s.mdb.Iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())

	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 3000, numFound)
	it.Close()

	// iter to key>=prefix
	key = NewFlakeKey(TableJobRunning, s.mdb.NextId())
	it = s.mdb.Iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())

	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 2000, numFound)
	it.Close()

	// again
	key = NewFlakeKey(TableJobRunning, s.mdb.NextId())
	it = s.mdb.Iterator()
	defer it.Close()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())

	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 2000, numFound)
	it.Close()
}

func (s *DBTestSuite) TestTimeTravelId() {
	// Given old time, should return the same time travel id
	dt := "2009-06-25T18:23:38Z"
	t, _ := time.Parse(time.RFC3339, dt)

	fid1 := s.mdb.TimeTravelId(t)
	for i := 0; i < 100; i++ {
		fid2 := s.mdb.TimeTravelId(t)
		assert.Equal(s.T(), string(fid1[:]), string(fid2[:]))
	}

	fid1 = s.mdb.TimeTravelReverseId(t)
	for i := 0; i < 100; i++ {
		fid2 := s.mdb.TimeTravelReverseId(t)
		assert.Equal(s.T(), string(fid1[:]), string(fid2[:]))
	}
}
