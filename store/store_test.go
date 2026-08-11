package store

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type DBTestSuite struct {
	suite.Suite

	rdb    *Store
	dbpath string
}

func (s *DBTestSuite) iterator() *Iterator {
	s.T().Helper()
	iter, err := s.rdb.Iterator()
	s.Require().NoError(err)
	return iter
}

func TestDBTestSuite(t *testing.T) {
	suite.Run(t, new(DBTestSuite))
}

func TestStoreOptionsShareLevelConfiguration(t *testing.T) {
	storeOptions := NewStoreOptions()

	assert.Len(t, storeOptions.Levels, 7)
	assert.Equal(t, 32<<10, storeOptions.Levels[0].BlockSize)
	assert.Equal(t, 256<<10, storeOptions.Levels[0].IndexBlockSize)
	assert.NotNil(t, storeOptions.Levels[0].FilterPolicy)
	assert.Nil(t, storeOptions.Levels[6].FilterPolicy)

	// Open applies EnsureDefaults; pin the expected default target
	// file sizes (2 MB at L0, doubling per level) to match the
	// pebble v1 configuration. Clone first so the defaults filled in
	// here do not leak into the assertions below.
	effective := storeOptions.Clone()
	effective.EnsureDefaults()
	wantSizes := []int64{2 << 20, 4 << 20, 8 << 20, 16 << 20, 32 << 20, 64 << 20, 128 << 20}
	for i, want := range wantSizes {
		assert.Equal(t, want, effective.TargetFileSizes[i], "TargetFileSizes[%d]", i)
	}

	assert.Equal(t, 2, storeOptions.L0CompactionThreshold)
	assert.Equal(t, 1000, storeOptions.L0StopWritesThreshold)
	assert.NotNil(t, storeOptions.CompactionConcurrencyRange)
	lower, upper := storeOptions.CompactionConcurrencyRange()
	assert.Equal(t, 1, lower)
	assert.Equal(t, 3, upper)
}

func TestStoreCloseIsConcurrentAndIdempotent(t *testing.T) {
	db, err := NewStore(t.TempDir())
	require.NoError(t, err)

	const callers = 16
	start := make(chan struct{})
	panics := make(chan any, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			defer func() {
				panics <- recover()
			}()
			<-start
			db.Close()
		}()
	}
	close(start)
	wg.Wait()
	close(panics)

	for recovered := range panics {
		if recovered != nil {
			t.Fatalf("concurrent Store.Close panicked: %v", recovered)
		}
	}
}

func TestNewStoreReturnsOperationalFailures(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		parent := t.TempDir()
		notDirectory := filepath.Join(parent, "file")
		assert.NoError(t, os.WriteFile(notDirectory, []byte("x"), 0600))

		db, err := NewStore(filepath.Join(notDirectory, "db"))
		assert.Nil(t, db)
		assert.Error(t, err)
	})

	t.Run("open", func(t *testing.T) {
		path := t.TempDir()
		first, err := NewStore(path)
		require.NoError(t, err)
		defer first.Close()

		second, err := NewStore(path)
		assert.Nil(t, second)
		assert.Error(t, err)
	})
}

func TestCloseWithErrorReturnsPebbleCloseFailure(t *testing.T) {
	db, err := NewStore(t.TempDir())
	require.NoError(t, err)
	assert.NoError(t, db.Set([]byte("key"), []byte("value")))
	iter, err := db.rdb.NewIter(nil)
	assert.NoError(t, err)
	defer iter.Close()

	err = db.CloseWithError()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "leaked iterators")
	assert.Equal(t, err, db.CloseWithError(), "repeated close must return the original result")
}

func (s *DBTestSuite) SetupTest() {
	log.Println("setup tests...")
	dbpath := os.TempDir() + "/testffdb2"
	rdb, err := NewStore(dbpath)
	s.Require().NoError(err)
	s.rdb = rdb
}

func (s *DBTestSuite) TearDownTest() {
	log.Println("teardown tests...")
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
	assert.ErrorIs(s.T(), err, ErrNotFound)
	assert.Nil(s.T(), value)
}

func (s *DBTestSuite) TestGetDistinguishesEmptyValueFromMissingKey() {
	assert.NoError(s.T(), s.rdb.Put([]byte("empty"), nil))

	value, err := s.rdb.Get([]byte("empty"))
	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), value)
	assert.Empty(s.T(), value)

	value, err = s.rdb.Get([]byte("missing"))
	assert.ErrorIs(s.T(), err, ErrNotFound)
	assert.Nil(s.T(), value)
}

func TestExistsReturnsReadErrors(t *testing.T) {
	db, err := NewStore(t.TempDir())
	assert.NoError(t, err)
	assert.NoError(t, db.CloseWithError())

	exists, err := db.Exists([]byte("key"))
	assert.Error(t, err)
	assert.False(t, exists)
}

func TestIteratorCreationReturnsErrors(t *testing.T) {
	db, err := NewStore(t.TempDir())
	assert.NoError(t, err)
	assert.NoError(t, db.CloseWithError())

	iter, err := db.Iterator()
	assert.Error(t, err)
	assert.Nil(t, iter)
}

func (s *DBTestSuite) TestSetSyncConcurrentWrites() {
	const writes = 100
	errs := make(chan error, writes)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := range writes {
			s.rdb.SetSync(i%2 == 0)
		}
	}()
	go func() {
		defer wg.Done()
		for i := range writes {
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
	err := s.rdb.Put([]byte("key1"), []byte("value1"))
	assert.Nil(s.T(), err)

	value, err := s.rdb.Get([]byte("key1"))
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "value1", string(value))

	// With large key
	key := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdegfhijklmnopqrstuvwxyz")
	err = s.rdb.Put(key, []byte("value2"))
	assert.Nil(s.T(), err)

	value, err = s.rdb.Get(key)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "value2", string(value))
}

func (s *DBTestSuite) TestIteration() {
	// Giving meta store, When iterator data, it should find all keys
	prefix := []byte("job:feed:")
	key1 := fmt.Sprintf("job:feed:%s", "key1")
	key2 := fmt.Sprintf("job:feed:%s", "key2")

	err := s.rdb.Put([]byte(key1), []byte("value1"))
	assert.Nil(s.T(), err)

	err = s.rdb.Put([]byte(key2), []byte("value2"))
	assert.Nil(s.T(), err)

	opts := PrefixIteratorOptions(prefix)
	iter, err := newIterator(s.rdb.rdb, opts)
	assert.NoError(s.T(), err)
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
	for range 3 {
		key := NewFlakeKey(TableJobFeed, s.rdb.NextId())
		s.rdb.Put(key.Bytes(), []byte("value1"))
	}

	for range 2 {
		key := NewFlakeKey(TableJobRunning, s.rdb.NextId())
		s.rdb.Put(key.Bytes(), []byte("value2"))
	}

	key := NewFlakeKey(TableMax, s.rdb.NextId())
	s.rdb.Put(key.Bytes(), []byte("value3"))

	key = NewFlakeKey(TableJobFeed, s.rdb.NextId())
	it := s.iterator()
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
	// s.rdb.Close()
	s.SetupTest()

	// iter to key>=prefix
	key = NewFlakeKey(TableJobFeed, s.rdb.NextId())
	it = s.iterator()
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
	key = NewFlakeKey(TableJobFeed, s.rdb.NextId())
	it = s.iterator()
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
	key = NewFlakeKey(TableJobRunning, s.rdb.NextId())
	it = s.iterator()
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
	key = NewFlakeKey(TableJobRunning, s.rdb.NextId())
	it = s.iterator()
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
	batch := s.rdb.rdb.NewBatch()
	for range 1000 {
		key := NewFlakeKey(TableJobFeed, s.rdb.NextId())
		batch.Set(key.Bytes(), []byte("value1"), pebble.NoSync)
	}

	for range 1000 {
		key := NewFlakeKey(TableJobRunning, s.rdb.NextId())
		batch.Set(key.Bytes(), []byte("value2"), pebble.NoSync)
	}

	for range 1000 {
		key := NewFlakeKey(TableMax, s.rdb.NextId())
		batch.Set(key.Bytes(), []byte("value3"), pebble.NoSync)
	}
	batch.Commit(pebble.Sync)
	batch.Close()

	key := NewFlakeKey(TableJobFeed, s.rdb.NextId())
	it := s.iterator()
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
	// s.rdb.Close()
	s.SetupTest()

	// iter to key>=prefix
	key = NewFlakeKey(TableJobFeed, s.rdb.NextId())
	it = s.iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())

	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 3000, numFound)
	it.Close()

	key = NewFlakeKey(TableJobFeed, s.rdb.NextId())
	it = s.iterator()
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
	key = NewFlakeKey(TableJobRunning, s.rdb.NextId())
	it = s.iterator()
	numFound = 0
	it.SeekGE(key.Prefix().Bytes())

	for ; it.Valid(); it.Next() {
		it.Key()
		numFound++
	}
	assert.Nil(s.T(), it.Error())
	assert.Equal(s.T(), 2000, numFound)
	it.Close()

	key = NewFlakeKey(TableJobRunning, s.rdb.NextId())
	it = s.iterator()
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
	// s.rdb.Put(maxKey, []byte(""))
	batch := s.rdb.rdb.NewBatch()
	for range 1000 {
		key := NewFlakeKey(TableJobFeed, s.rdb.NextId())
		batch.Set(key.Bytes(), []byte("value1"), pebble.NoSync)
	}

	for range 1000 {
		key := NewFlakeKey(TableJobRunning, s.rdb.NextId())
		batch.Set(key.Bytes(), []byte("value2"), pebble.NoSync)
	}

	for range 1000 {
		key := NewFlakeKey(TableMax, s.rdb.NextId())
		batch.Set(key.Bytes(), []byte("value3"), pebble.NoSync)
	}
	batch.Commit(pebble.Sync)
	batch.Close()

	key := NewFlakeKey(TableJobFeed, s.rdb.NextId())
	it := s.iterator()
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
	// s.rdb.Close()
	s.SetupTest()

	// iter to key>=prefix
	key = NewFlakeKey(TableJobFeed, s.rdb.NextId())
	it = s.iterator()
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
	key = NewFlakeKey(TableJobFeed, s.rdb.NextId())
	it = s.iterator()
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
	key = NewFlakeKey(TableJobRunning, s.rdb.NextId())
	it = s.iterator()
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
	key = NewFlakeKey(TableJobRunning, s.rdb.NextId())
	it = s.iterator()
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

	fid1 := s.rdb.TimeTravelId(t)
	for range 100 {
		fid2 := s.rdb.TimeTravelId(t)
		assert.Equal(s.T(), string(fid1[:]), string(fid2[:]))
	}

	fid1 = s.rdb.TimeTravelReverseId(t)
	for range 100 {
		fid2 := s.rdb.TimeTravelReverseId(t)
		assert.Equal(s.T(), string(fid1[:]), string(fid2[:]))
	}
}
