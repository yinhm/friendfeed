package model

import (
	"errors"
	"log"
	"os"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/store/flake"
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
	db, err := store.NewStore(dbpath)
	s.Require().NoError(err)
	s.db = db

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
		// Id:   "yinhm",
		// Name: "yinhm",
		// Type: "user",
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
	sKey, err := PutEntry(s.db, e)
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

	sEntry, err := GetEntry(s.db, "2b43a9066074d120ed2e45494eea1797")
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), mEntry.Id, sEntry.Id)

	profileUUID, _ := uuid.FromString(p.Uuid)
	key := store.NewUUIDKey(TableEntryIndex, profileUUID)
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

	// produce duplicated entry issue when server moved
	oldNewWorkerId := flake.NewWorkerId
	flake.NewWorkerId = flake.NewRandWorkerId
	n, err = s.db.ForwardScan(key.Bytes(), func(i int, k, v []byte) error {
		return nil
	})
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 0, n)

	_, err = PutEntry(s.db, e)
	assert.Nil(s.T(), err)

	n, err = s.db.ForwardScan(key.Bytes(), func(i int, k, v []byte) error {
		return nil
	})
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 1, n)

	_, err = PutEntry(s.db, e)
	assert.Nil(s.T(), err)

	n, err = s.db.ForwardScan(key.Bytes(), func(i int, k, v []byte) error {
		return nil
	})
	assert.Nil(s.T(), err)
	// it would be 2 if we not delete the old index
	assert.Equal(s.T(), 1, n)

	// restore NewWorkerId func otherwise will break other tests
	flake.NewWorkerId = oldNewWorkerId
}

func (s *TableTestSuite) TestArchiveHistory() {
	//No archive history
	_, err := GetArchiveHistory(s.db, "not-exists")
	assert.NotNil(s.T(), err)
}

func (s *TableTestSuite) TestDeleteEntryPropagatesReadError() {
	entryUUID := uuid.Must(uuid.NewV4())
	key := Entry.PrefixAppend(entryUUID.Bytes())
	err := s.db.Put(key, []byte{0xff})
	assert.NoError(s.T(), err)

	err = DeleteEntry(s.db, entryUUID.String())
	assert.Error(s.T(), err)
	assert.False(s.T(), errors.Is(err, ErrNotFound))

	// Blind deletion remains successful for a genuinely missing entry.
	missingUUID := uuid.Must(uuid.NewV4())
	err = DeleteEntry(s.db, missingUUID.String())
	assert.NoError(s.T(), err)
}

func (s *TableTestSuite) TestFanoutEntryAndDeleteFanoutEntry() {
	userUUID := uuid.Must(uuid.NewV4())
	feedUUID := uuid.Must(uuid.NewV4())
	followerUUID := uuid.Must(uuid.NewV4())
	followerKey := NewKeyFrom(Follower.Prefix, feedUUID.Bytes(), followerUUID.Bytes())
	assert.NoError(s.T(), s.db.Put(followerKey, []byte("1")))

	entryTime := time.Now().UTC().Truncate(time.Second)
	entryKey := Entry.PrefixAppend(uuid.Must(uuid.NewV4()).Bytes())
	n, err := FanoutEntry(s.db, userUUID, feedUUID, entryTime, entryKey)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), 1, n)

	userTimeline := TimelineUUID(userUUID)
	followerTimeline := UniqueKeyFrom(store.Key(followerUUID.Bytes()).String(), "user", "timeline")
	assert.Equal(s.T(), 1, s.countEntryIndex(userTimeline))
	assert.Equal(s.T(), 1, s.countEntryIndex(followerTimeline))

	n, err = DeleteFanoutEntry(s.db, userUUID, feedUUID, entryTime, entryKey)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), 1, n)
	assert.Equal(s.T(), 0, s.countEntryIndex(userTimeline))
	assert.Equal(s.T(), 0, s.countEntryIndex(followerTimeline))
}

func (s *TableTestSuite) TestUpdateFollowerTimelinesPropagatesUpdateError() {
	feedUUID := uuid.Must(uuid.NewV4())
	followerUUID := uuid.Must(uuid.NewV4())
	followerKey := NewKeyFrom(Follower.Prefix, feedUUID.Bytes(), followerUUID.Bytes())
	assert.NoError(s.T(), s.db.Put(followerKey, []byte("1")))

	wantErr := errors.New("timeline update failed")
	n, err := updateFollowerTimelines(s.db, feedUUID, func(uuid.UUID) error {
		return wantErr
	})
	assert.Zero(s.T(), n)
	assert.ErrorIs(s.T(), err, wantErr)
}

func (s *TableTestSuite) TestPutDeleteEntryMaintainsAuthorTimeline() {
	authorUUID := uuid.Must(uuid.NewV4())
	assert.NoError(s.T(), TouchTimelineState(s.db, authorUUID, time.Now().UTC()))
	entryUUID := uuid.Must(uuid.NewV4())
	entry := &pb.Entry{
		Id:          entryUUID.String(),
		Date:        time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		ProfileUuid: authorUUID.String(),
		From:        &pb.Feed{Uuid: authorUUID.String(), Id: "author"},
	}

	_, err := PutEntry(s.db, entry)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), 1, s.countEntryIndex(authorUUID))
	assert.Equal(s.T(), 1, s.countTimelineIndex(authorUUID))

	assert.NoError(s.T(), DeleteEntry(s.db, entryUUID.String()))
	assert.Equal(s.T(), 0, s.countEntryIndex(authorUUID))
	assert.Equal(s.T(), 1, s.countTimelineIndex(authorUUID), "timeline deletion is lazy")
}

func (s *TableTestSuite) TestPutEntryValidationFailureDoesNotPersistEntry() {
	authorUUID := uuid.Must(uuid.NewV4())
	entryUUID := uuid.Must(uuid.NewV4())
	_, err := PutEntry(s.db, &pb.Entry{
		Id:          entryUUID.String(),
		Date:        "not-rfc3339",
		ProfileUuid: authorUUID.String(),
	})
	assert.Error(s.T(), err)

	_, err = GetEntry(s.db, entryUUID.String())
	assert.ErrorIs(s.T(), err, ErrNotFound)
	assert.Equal(s.T(), 0, s.countEntryIndex(authorUUID))
	assert.Equal(s.T(), 0, s.countTimelineIndex(authorUUID))
}

func TestEntryIndexKeepsDistinctEntriesInSameSecond(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	assert.NoError(t, err)
	t.Cleanup(db.Close)

	authorUUID := uuid.Must(uuid.NewV4())
	entryTime := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	for range 2 {
		entryUUID := uuid.Must(uuid.NewV4())
		_, err := PutEntry(db, &pb.Entry{
			Id:          entryUUID.String(),
			Date:        entryTime,
			ProfileUuid: authorUUID.String(),
			From:        &pb.Feed{Uuid: authorUUID.String(), Id: "author"},
		})
		assert.NoError(t, err)
	}

	var indexedEntryKeys []string
	_, err = db.ForwardScan(store.NewUUIDKey(TableEntryIndex, authorUUID).Bytes(), func(_ int, _ []byte, value []byte) error {
		indexedEntryKeys = append(indexedEntryKeys, store.Key(value).String())
		return nil
	})
	assert.NoError(t, err)
	assert.Len(t, indexedEntryKeys, 2)
}

func TestEntryIndexRejectsNoncanonicalEntryKey(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	err = EntryIndex.Index(db, uuid.Must(uuid.NewV4()), time.Now(), NewKeyFrom(Entry.Prefix, []byte(uuid.Must(uuid.NewV4()).String())))
	require.ErrorContains(t, err, "noncanonical Entry key length")
}

func (s *TableTestSuite) TestPutDeleteGroupEntryMaintainsAllIndexes() {
	authorUUID := uuid.Must(uuid.NewV4())
	groupUUID := uuid.Must(uuid.NewV4())
	followerUUID := uuid.Must(uuid.NewV4())
	entryUUID := uuid.Must(uuid.NewV4())
	followerKey := NewKeyFrom(Follower.Prefix, groupUUID.Bytes(), followerUUID.Bytes())
	assert.NoError(s.T(), s.db.Put(followerKey, []byte("1")))
	assert.NoError(s.T(), TouchTimelineState(s.db, authorUUID, time.Now().UTC()))
	assert.NoError(s.T(), TouchTimelineState(s.db, followerUUID, time.Now().UTC()))

	entry := &pb.Entry{
		Id:          entryUUID.String(),
		Date:        time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		ProfileUuid: authorUUID.String(),
		FeedUuid:    groupUUID.String(),
		From:        &pb.Feed{Uuid: authorUUID.String(), Id: "author"},
	}
	_, err := PutEntry(s.db, entry)
	assert.NoError(s.T(), err)
	for name, indexUUID := range map[string]uuid.UUID{
		"author": authorUUID,
		"group":  groupUUID,
	} {
		assert.Equal(s.T(), 1, s.countEntryIndex(indexUUID), name)
	}
	assert.Equal(s.T(), 1, s.countTimelineIndex(authorUUID), "author timeline")
	assert.Equal(s.T(), 1, s.countTimelineIndex(followerUUID), "follower timeline")

	assert.NoError(s.T(), DeleteEntry(s.db, entryUUID.String()))
	for name, indexUUID := range map[string]uuid.UUID{
		"author": authorUUID,
		"group":  groupUUID,
	} {
		assert.Equal(s.T(), 0, s.countEntryIndex(indexUUID), name)
	}
	assert.Equal(s.T(), 1, s.countTimelineIndex(authorUUID), "author timeline lazy cleanup")
	assert.Equal(s.T(), 1, s.countTimelineIndex(followerUUID), "follower timeline lazy cleanup")
}

func (s *TableTestSuite) countEntryIndex(timelineUUID uuid.UUID) int {
	n, err := s.db.ForwardScan(store.NewUUIDKey(TableEntryIndex, timelineUUID).Bytes(), func(i int, k, v []byte) error {
		return nil
	})
	assert.NoError(s.T(), err)
	return n
}

func (s *TableTestSuite) countTimelineIndex(viewerUUID uuid.UUID) int {
	n, err := s.db.ForwardScan(TimelineIndexPrefix(viewerUUID), func(int, []byte, []byte) error { return nil })
	assert.NoError(s.T(), err)
	return n
}
