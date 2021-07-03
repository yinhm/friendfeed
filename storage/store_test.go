package store

import (
	"fmt"
	"log"
	"os"
	"path"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	pb "github.com/yinhm/friendfeed/proto"
	"github.com/yinhm/friendfeed/storage/flake"
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
	s.mdb = NewMetaStore(path.Join(dbpath, "meta"))
}

func (s *DBTestSuite) TearDownTest() {
	log.Println("teardown tests...")
	s.mdb.Close()
	err := os.RemoveAll(s.mdb.dbpath)
	if err != nil {
		log.Println("can not remove test db.")
	}

	s.rdb.Close()
	err = os.RemoveAll(s.rdb.dbpath)
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
	s.mdb.Close()
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
	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableJobFeed, s.mdb.NextId())
		s.mdb.Put(key.Bytes(), []byte("value1"))
	}

	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableJobRunning, s.mdb.NextId())
		s.mdb.Put(key.Bytes(), []byte("value2"))
	}

	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableMax, s.mdb.NextId())
		s.mdb.Put(key.Bytes(), []byte("value3"))
	}

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
	s.mdb.Close()
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
	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableJobFeed, s.mdb.NextId())
		s.mdb.Put(key.Bytes(), []byte("value1"))
	}

	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableJobRunning, s.mdb.NextId())
		s.mdb.Put(key.Bytes(), []byte("value2"))
	}

	for i := 0; i < 1000; i++ {
		key := NewFlakeKey(TableMax, s.mdb.NextId())
		s.mdb.Put(key.Bytes(), []byte("value3"))
	}

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
	s.mdb.Close()
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

func (s *DBTestSuite) TestOAuthUser() {
	// "Given OAuth User, should save
	ptu := &pb.OAuthUser{
		UserId:      "12345",
		Name:        "foobar",
		NickName:    "foo bar",
		Email:       "foo@bar.com",
		AccessToken: "f o o b a r",
		Provider:    "twitter",
	}

	got, err := PutOAuthUser(s.mdb, ptu)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), ptu.UserId, got.UserId)
	assert.Equal(s.T(), ptu.Provider, got.Provider)

	key := NewMetaKey(TableOAuthTwitter, ptu.UserId)
	rawdata, err := s.mdb.Get(key.Bytes())
	assert.Nil(s.T(), err)
	assert.NotEqual(s.T(), "", rawdata)
}

func (s *DBTestSuite) TestArchiveHistory() {
	//No archive history
	job, err := GetArchiveHistory(s.mdb, "not-exists")
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "", job.Key)
	assert.NotEqual(s.T(), "done", job.Status)
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

func (s *DBTestSuite) TestPutEntry() {
	p := &pb.Profile{
		Uuid: "c6f8dca854f011ddb489003048343a40",
		Id:   "yinhm",
		Name: "yinhm",
		Type: "user",
	}

	feed := &pb.Feed{
		Id:   "yinhm",
		Name: "yinhm",
		Type: "user",
	}

	e := &pb.Entry{
		Body:        "张无忌对张三丰说：“太师父，武当山的生活太寂寞了，只有清风和明月两个朋友能陪我玩。”张三丰叹了口气：“已经很不错啦，至少还有清风明月呢。想当年我在少林寺的时候，也是只有两个朋友，其中一个也叫清风……”“那另一个呢？”“叫心相印。”…",
		Id:          "e/2b43a9066074d120ed2e45494eea1797",
		Date:        "2012-09-07T07:40:22Z",
		Url:         "http://friendfeed.com/yinhm/2b43a906/rt-trojansj",
		From:        feed,
		ProfileUuid: "c6f8dca854f011ddb489003048343a40",
	}

	// Put entry"
	// fresh put
	_, err := PutEntry(s.rdb, e, false)
	assert.Nil(s.T(), err)

	// put exists entry
	_, err = PutEntry(s.rdb, e, false)
	_, ok := err.(*Error)
	assert.True(s.T(), ok)

	// force put
	e.Id = "e/ab439960a83546c683fd989a40a68462"
	// fake new falkeid
	e.Date = "2013-09-07T07:40:22Z"

	_, err = PutEntry(s.rdb, e, false)
	assert.Nil(s.T(), err)

	// force put exists entry
	_, err = PutEntry(s.rdb, e, true)
	assert.Nil(s.T(), err)

	for i := 0; i < 100; i++ {
		_, err = PutEntry(s.rdb, e, true)
		assert.Nil(s.T(), err)
	}

	uuid1, _ := uuid.FromString(p.Uuid)
	key := NewUUIDKey(TableReverseEntryIndex, uuid1)
	n, err := ForwardTableScan(s.rdb, key, func(i int, k, v []byte) error {
		return nil
	})
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 2, n)

	// produce duplicated entry issue when server moved
	oldNewWorkerId := flake.NewWorkerId
	flake.NewWorkerId = flake.NewRandWorkerId
	// rdb.idGen.WorkerId = flake.NewRandWorkerId()
	// force put exists entry
	_, err = PutEntry(s.rdb, e, true)
	assert.Nil(s.T(), err)

	n, err = ForwardTableScan(s.rdb, key, func(i int, k, v []byte) error {
		return nil
	})
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 2, n)

	// restore NewWorkerId func otherwise will break other tests
	flake.NewWorkerId = oldNewWorkerId
}
