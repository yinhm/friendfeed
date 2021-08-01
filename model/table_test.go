package model

import (
	"log"
	"os"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
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

	search.InitMockIndexService(dbpath)
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

	_, err := Stock.Put(s.db, store.KeyFromString(farmHash), p)
	assert.Nil(s.T(), err)

	farm := new(pb.Feed)
	err = Stock.Get(s.db, store.KeyFromString(farmHash), farm)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "000001", farm.Name)
}

func (s *TableTestSuite) TestPutEntry() {
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
		Id:          "2b43a9066074d120ed2e45494eea1797",
		Date:        "2012-09-07T07:40:22Z",
		Url:         "http://friendfeed.com/yinhm/2b43a906/rt-trojansj",
		From:        feed,
		ProfileUuid: "c6f8dca854f011ddb489003048343a40",
	}

	// put new entry
	sKey, err := store.PutEntry(s.db, e, false)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "000000032b43a9066074d120ed2e45494eea1797", sKey.String())

	// put exists entry
	eKey, err := PutEntry(s.db, e)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "000000032b43a9066074d120ed2e45494eea1797", eKey.String())
	mEntry := new(pb.Entry)
	// nil????
	err = Entry.Get(s.db, store.KeyFromString("000000032b43a9066074d120ed2e45494eea1797"), mEntry)
	assert.NotNil(s.T(), err)
	err = Entry.Get(s.db, store.KeyFromString("2b43a9066074d120ed2e45494eea1797"), mEntry)
	assert.Nil(s.T(), err)

	sEntry, err := store.GetEntry(s.db, "2b43a9066074d120ed2e45494eea1797")
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), mEntry.Id, sEntry.Id)

	uuid1, _ := uuid.FromString(p.Uuid)
	key := store.NewUUIDKey(TableEntryIndex, uuid1)
	n, err := s.db.ForwardScan(key.Bytes(), func(i int, k, v []byte) error {
		return nil
	})
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 1, n)

	// model, entry get -> delete -> get
	entry, err := GetEntry(s.db, "2b43a9066074d120ed2e45494eea1797")
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "c6f8dca854f011ddb489003048343a40", entry.ProfileUuid)
	err = DeleteEntry(s.db, "2b43a9066074d120ed2e45494eea1797")
	assert.Nil(s.T(), err)
	_, err = GetEntry(s.db, "2b43a9066074d120ed2e45494eea1797")
	assert.NotNil(s.T(), err)
	err = DeleteEntry(s.db, "2b43a9066074d120ed2e45494eea1797")
	assert.Nil(s.T(), err) // blind delete

	// // produce duplicated entry issue when server moved
	// oldNewWorkerId := flake.NewWorkerId
	// flake.NewWorkerId = flake.NewRandWorkerId
	// _, err = PutEntry(s.db, e)
	// assert.Nil(s.T(), err)

	// n, err = store.ForwardTableScan(s.db, key, func(i int, k, v []byte) error {
	// 	return nil
	// })
	// assert.Nil(s.T(), err)
	// assert.Equal(s.T(), 1, n)

	// // restore NewWorkerId func otherwise will break other tests
	// flake.NewWorkerId = oldNewWorkerId
}
