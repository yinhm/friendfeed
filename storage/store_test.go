package store

import (
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	. "github.com/smartystreets/goconvey/convey"
	pb "github.com/yinhm/friendfeed/proto"
	"github.com/yinhm/friendfeed/storage/flake"
	rocksdb "github.com/yinhm/gorocksdb"
)

var (
	rdb    *Store
	mdb    *Store
	dbpath string
)

func setup() {
	dbpath := os.TempDir() + "/fftestdb"
	rdb = NewStore(dbpath)
	mdb = NewMetaStore(dbpath + "/meta")
}

func teardown() {
	if !rdb.closed {
		rdb.Close()
		if err := rdb.Destroy(); err != nil {
			log.Fatalf("fail on destroy rdb: %s", err)
		}
	}
	if !mdb.closed {
		mdb.Close()
		if err := mdb.Destroy(); err != nil {
			log.Fatalf("fail on destroy mdb: %s", err)
		}
	}
}

func TestStore(t *testing.T) {
	setup()
	defer teardown()

	Convey("When put then get value, it should be equal", t, func() {
		err := rdb.Put([]byte("key1"), []byte("value1"))
		So(err, ShouldBeNil)

		value, err := rdb.Get([]byte("key1"))
		So(err, ShouldBeNil)

		So(string(value), ShouldEqual, "value1")

		value, err = rdb.Get([]byte("key2"))
		So(err, ShouldBeNil)
		So(value, ShouldEqual, nil)
	})
}

func TestMetaStore(t *testing.T) {
	setup()
	defer teardown()

	Convey("Giving meta store", t, func() {
		err := mdb.Put([]byte("key1"), []byte("value1"))
		So(err, ShouldBeNil)
		value, err := mdb.Get([]byte("key1"))
		So(err, ShouldBeNil)
		So(string(value), ShouldEqual, "value1")

		Convey("With large key", func() {
			key := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdegfhijklmnopqrstuvwxyz")
			err := mdb.Put(key, []byte("value2"))
			So(err, ShouldBeNil)
			value, err := mdb.Get(key)
			So(err, ShouldBeNil)
			So(string(value), ShouldEqual, "value2")
		})
	})
}

func TestIteration(t *testing.T) {
	setup()
	defer teardown()

	Convey("Giving meta store, When iterator data, it should find all keys", t, func() {
		key1 := fmt.Sprintf("job:feed:%s", "key1")
		key2 := fmt.Sprintf("job:feed:%s", "key2")

		So(mdb.Put([]byte(key1), []byte("value1")), ShouldBeNil)
		So(mdb.Put([]byte(key2), []byte("value2")), ShouldBeNil)

		iter := mdb.Iterator()
		defer iter.Close()

		So(func() { iter.Seek([]byte(key1)) }, ShouldNotPanic)
		So(iter.Valid(), ShouldBeTrue)

		So(string(iter.Key().Data()), ShouldEqual, key1)
		So(string(iter.Value().Data()), ShouldEqual, "value1")

		iter.Next()
		So(iter.Valid(), ShouldBeTrue)
		So(string(iter.Key().Data()), ShouldEqual, key2)
		So(string(iter.Value().Data()), ShouldEqual, "value2")

		iter.Next()
		So(iter.Valid(), ShouldBeFalse)

		err := iter.Err()
		So(err, ShouldBeNil)
	})
}

func TestIteration2(t *testing.T) {
	setup()
	defer teardown()

	Convey("Giving meta store, When iterator data, it should find all keys", t, func() {
		Convey("first iter", func() {
			for i := 0; i < 3; i++ {
				key := NewFlakeKey(TableJobFeed, mdb.NextId())
				mdb.Put(key.Bytes(), []byte("value1"))
			}

			for i := 0; i < 2; i++ {
				key := NewFlakeKey(TableJobRunning, mdb.NextId())
				mdb.Put(key.Bytes(), []byte("value2"))
			}

			key := NewFlakeKey(TableMax, mdb.NextId())
			mdb.Put(key.Bytes(), []byte("value3"))

			key = NewFlakeKey(TableJobFeed, mdb.NextId())
			it := mdb.Iterator()
			defer it.Close()
			it.Seek(key.Prefix().Bytes())

			numFound := 0
			for ; it.Valid(); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			// mdb switched to Block-based format
			// So(numFound, ShouldEqual, 3)
			So(numFound, ShouldEqual, 6)
		})

		// WARN: must scoped or trigger assertion `is_last_reference' failed
		// due to it not closed.
		Convey("demonstrate the inconsistent behaviour when reopen db", func() {
			// reopen
			rdb.Close()
			mdb.Close()
			setup()
			defer teardown()

			// iter to key>=prefix
			key := NewFlakeKey(TableJobFeed, mdb.NextId())
			it := mdb.Iterator()
			defer it.Close()
			numFound := 0
			it.Seek(key.Prefix().Bytes())

			for ; it.Valid(); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 6)

			// so we need to use ValidForPrefix
			key = NewFlakeKey(TableJobFeed, mdb.NextId())
			it = mdb.Iterator()
			defer it.Close()
			numFound = 0
			it.Seek(key.Prefix().Bytes())

			for ; it.ValidForPrefix(key.Prefix().Bytes()); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 3)

			// iter to key>=prefix
			key = NewFlakeKey(TableJobRunning, mdb.NextId())
			it = mdb.Iterator()
			defer it.Close()
			numFound = 0
			it.Seek(key.Prefix().Bytes())

			for ; it.Valid(); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 3)

			// iter.ValidForPrefix
			key = NewFlakeKey(TableJobRunning, mdb.NextId())
			it = mdb.Iterator()
			defer it.Close()
			numFound = 0
			it.Seek(key.Prefix().Bytes())

			for ; it.ValidForPrefix(key.Prefix().Bytes()); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 2)
		})
	})
}

func TestRockStorePrefixSeek(t *testing.T) {
	setup()
	defer teardown()

	Convey("Giving meta store", t, func() {
		Convey("First iteration: populate data", func() {
			for i := 0; i < 1000; i++ {
				key := NewFlakeKey(TableJobFeed, mdb.NextId())
				mdb.Put(key.Bytes(), []byte("value1"))
			}

			for i := 0; i < 1000; i++ {
				key := NewFlakeKey(TableJobRunning, mdb.NextId())
				mdb.Put(key.Bytes(), []byte("value2"))
			}

			for i := 0; i < 1000; i++ {
				key := NewFlakeKey(TableMax, mdb.NextId())
				mdb.Put(key.Bytes(), []byte("value3"))
			}

			ro := rocksdb.NewDefaultReadOptions()
			// ro.prefix_seek = true is on by default
			key := NewFlakeKey(TableJobFeed, mdb.NextId())
			it := mdb.rdb.NewIterator(ro)
			defer it.Close()
			it.Seek(key.Prefix().Bytes())

			numFound := 0
			for ; it.Valid(); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			// mdb switched to Block-based format
			//So(numFound, ShouldEqual, 1000)
			So(numFound, ShouldEqual, 3000)
		})

		Convey("Second iteration: reopen db", func() {
			// reopen
			rdb.Close()
			mdb.Close()
			setup()
			defer teardown()

			// iter to key>=prefix
			ro := rocksdb.NewDefaultReadOptions()
			// ro.prefix_seek = true is on by default
			key := NewFlakeKey(TableJobFeed, mdb.NextId())
			it := mdb.rdb.NewIterator(ro)
			defer it.Close()
			numFound := 0
			it.Seek(key.Prefix().Bytes())

			for ; it.Valid(); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 3000)

			// so we need to use ValidForPrefix
			key = NewFlakeKey(TableJobFeed, mdb.NextId())
			it = mdb.Iterator()
			defer it.Close()
			numFound = 0
			it.Seek(key.Prefix().Bytes())

			for ; it.ValidForPrefix(key.Prefix().Bytes()); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 1000)

			// iter to key>=prefix
			key = NewFlakeKey(TableJobRunning, mdb.NextId())
			it = mdb.Iterator()
			defer it.Close()
			numFound = 0
			it.Seek(key.Prefix().Bytes())

			for ; it.Valid(); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 2000)

			// iter.ValidForPrefix
			key = NewFlakeKey(TableJobRunning, mdb.NextId())
			it = mdb.Iterator()
			defer it.Close()
			numFound = 0
			it.Seek(key.Prefix().Bytes())

			for ; it.ValidForPrefix(key.Prefix().Bytes()); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 1000)
		})
	})
}

func TestPrefixSeekWithDelimiterKey(t *testing.T) {
	setup()
	defer teardown()

	Convey("Giving meta store", t, func() {
		Convey("First iteration: populate data", func() {
			// @rdallman suggest this hack on gorocksdb issue #24
			// maxKey := []byte{
			// 	0xFF, 0xFF, 0xFF, 0xFF,
			// 	0xFF, 0xFF, 0xFF, 0xFF,
			// 	0xFF, 0xFF, 0xFF, 0xFF,
			// 	0xFF, 0xFF, 0xFF, 0xFF,
			// }
			// mdb.Put(maxKey, []byte(""))

			for i := 0; i < 1000; i++ {
				key := NewFlakeKey(TableJobFeed, mdb.NextId())
				mdb.Put(key.Bytes(), []byte("value1"))
			}

			for i := 0; i < 1000; i++ {
				key := NewFlakeKey(TableJobRunning, mdb.NextId())
				mdb.Put(key.Bytes(), []byte("value2"))
			}

			for i := 0; i < 1000; i++ {
				key := NewFlakeKey(TableMax, mdb.NextId())
				mdb.Put(key.Bytes(), []byte("value3"))
			}

			ro := rocksdb.NewDefaultReadOptions()
			// ro.prefix_seek = true is on by default
			key := NewFlakeKey(TableJobFeed, mdb.NextId())
			it := mdb.rdb.NewIterator(ro)
			defer it.Close()
			it.Seek(key.Prefix().Bytes())

			numFound := 0
			for ; it.Valid(); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			// mdb switched to Block-based format
			//So(numFound, ShouldEqual, 1000)
			So(numFound, ShouldEqual, 3000)
		})

		Convey("Second iteration: reopen db", func() {
			// reopen
			rdb.Close()
			mdb.Close()
			setup()
			defer teardown()

			// iter to key>=prefix
			ro := rocksdb.NewDefaultReadOptions()
			// ro.prefix_seek = true is on by default
			key := NewFlakeKey(TableJobFeed, mdb.NextId())
			it := mdb.rdb.NewIterator(ro)
			defer it.Close()
			numFound := 0
			it.Seek(key.Prefix().Bytes())

			for ; it.Valid(); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 3000)

			// so we need to use ValidForPrefix
			key = NewFlakeKey(TableJobFeed, mdb.NextId())
			it = mdb.Iterator()
			defer it.Close()
			numFound = 0
			it.Seek(key.Prefix().Bytes())

			for ; it.ValidForPrefix(key.Prefix().Bytes()); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 1000)

			// iter to key>=prefix
			key = NewFlakeKey(TableJobRunning, mdb.NextId())
			it = mdb.Iterator()
			defer it.Close()
			numFound = 0
			it.Seek(key.Prefix().Bytes())

			for ; it.Valid(); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 2000)

			// iter.ValidForPrefix
			key = NewFlakeKey(TableJobRunning, mdb.NextId())
			it = mdb.Iterator()
			defer it.Close()
			numFound = 0
			it.Seek(key.Prefix().Bytes())

			for ; it.ValidForPrefix(key.Prefix().Bytes()); it.Next() {
				kk := it.Key()
				kk.Free()
				numFound++
			}
			So(it.Err(), ShouldBeNil)
			So(numFound, ShouldEqual, 1000)
		})
	})
}

//-------------------------
// testing keys
//-------------------------

func TestPrefixTable(t *testing.T) {
	Convey("Giving prefix table, convert to bytes", t, func() {
		var p1 PrefixTable
		So(p1.Len(), ShouldEqual, 4)

		p := TableFeed
		So(p.Len(), ShouldEqual, 4)
		So(p.String(), ShouldEqual, "00000001")
		So(hex.EncodeToString(p.Bytes()), ShouldEqual, "00000001")
	})
}

func TestMetaKey(t *testing.T) {
	Convey("Giving meta key, When convert to bytes", t, func() {
		key := &MetaKey{TableOAuthTwitter, "foobar"}
		So(key.Len(), ShouldEqual, 10)
		So(key.Prefix().Len(), ShouldEqual, 4)
		So(key.String(), ShouldEqual, key.Prefix().String()+"foobar")
	})
}

func TestFlakeKey(t *testing.T) {
	Convey("Giving falke key, When convert to bytes", t, func() {
		fid := flake.Id{}
		suffix := hex.EncodeToString(fid[:])
		key := &FlakeKey{TableFeed, fid}
		So(key.Len(), ShouldEqual, 20)
		So(key.Prefix().Len(), ShouldEqual, 4)
		So(key.String(), ShouldEqual, "00000001"+suffix)
		So(hex.EncodeToString(key.Prefix().Bytes()), ShouldEqual, "00000001")

		key.PrefixTable = TableFeedinfo
		So(key.String(), ShouldEqual, "00000002"+suffix)
		So(hex.EncodeToString(key.Prefix().Bytes()), ShouldEqual, "00000002")

		key.Id[15] = 1
		suffix = hex.EncodeToString(key.Id[:])
		So(key.String(), ShouldEqual, "00000002"+suffix)
		So(hex.EncodeToString(key.Prefix().Bytes()), ShouldEqual, "00000002")
	})
}

func TestUUIDKey(t *testing.T) {
	Convey("Giving prefix, convert to bytes", t, func() {
		uuid1 := new(uuid.UUID)
		So(uuid1.String(), ShouldEqual, "00000000-0000-0000-0000-000000000000")

		id, err := uuid.FromString("c6f8dca8-54f0-11dd-b489-003048343a40")
		So(err, ShouldBeNil)
		So(hex.EncodeToString(id.Bytes()), ShouldEqual, hex.EncodeToString(id[:16][:]))

		prefix := NewUUIDKey(TableFeed, id)
		So(prefix.Len(), ShouldEqual, 20)

		uid := "c6f8dca854f011ddb489003048343a40"
		So(prefix.String(), ShouldEqual, "00000001"+uid)
		So(hex.EncodeToString(prefix.Bytes()), ShouldEqual, "00000001"+uid)
	})
}

func TestUUIDFlakeKey(t *testing.T) {
	Convey("Giving key, convert to bytes", t, func() {
		id, err := uuid.FromString("c6f8dca8-54f0-11dd-b489-003048343a40")
		So(err, ShouldBeNil)
		So(hex.EncodeToString(id.Bytes()), ShouldEqual, hex.EncodeToString(id[:16][:]))

		fid := flake.Id{}
		suffix := hex.EncodeToString(fid[:])
		key := NewUUIDFlakeKey(TableFeed, id, fid)
		So(key.Len(), ShouldEqual, 36)

		uid := "c6f8dca854f011ddb489003048343a40"
		So(key.String(), ShouldEqual, "00000001"+uid+suffix)
		So(hex.EncodeToString(key.Prefix().Bytes()), ShouldEqual, "00000001"+uid)
		So(string(key.Prefix().Bytes()), ShouldEqual, string(key.UUIDKey.Bytes()))

		key.UUIDKey.PrefixTable = TableFeedinfo
		So(key.String(), ShouldEqual, "00000002"+uid+suffix)
		So(hex.EncodeToString(key.Prefix().Bytes()), ShouldEqual, "00000002"+uid)

		key.Id[15] = 1
		suffix = hex.EncodeToString(key.Id[:])
		So(key.String(), ShouldEqual, "00000002"+uid+suffix)
		So(hex.EncodeToString(key.Prefix().Bytes()), ShouldEqual, "00000002"+uid)
	})
}

func TestOAuthUser(t *testing.T) {
	setup()
	defer teardown()

	Convey("Given OAuth User, should save", t, func() {
		ptu := &pb.OAuthUser{
			UserId:      "12345",
			Name:        "foobar",
			NickName:    "foo bar",
			Email:       "foo@bar.com",
			AccessToken: "f o o b a r",
			Provider:    "twitter",
		}

		got, err := UpdateOAuthUser(mdb, ptu)
		So(err, ShouldBeNil)
		So(got.UserId, ShouldEqual, ptu.UserId)
		So(got.Provider, ShouldEqual, ptu.Provider)

		key := NewMetaKey(TableOAuthTwitter, ptu.UserId)
		rawdata, err := mdb.Get(key.Bytes())
		So(err, ShouldBeNil)
		So(rawdata, ShouldNotEqual, "")
	})
}

func TestArchiveHistory(t *testing.T) {
	setup()
	defer teardown()

	Convey("No archive history", t, func() {
		job, err := GetArchiveHistory(mdb, "not-exists")
		So(err, ShouldBeNil)
		So(job.Key, ShouldEqual, "")
		So(job.Status, ShouldNotEqual, "done")
	})
}

func TestTimeTravelId(t *testing.T) {
	setup()
	defer teardown()

	Convey("Given old time, should return the same time travel id", t, func() {
		dt := "2009-06-25T18:23:38Z"
		t, _ := time.Parse(time.RFC3339, dt)

		fid1 := mdb.TimeTravelId(t)
		for i := 0; i < 100; i++ {
			fid2 := mdb.TimeTravelId(t)
			So(fid1, ShouldEqual, fid2)
		}

		fid1 = mdb.TimeTravelReverseId(t)
		for i := 0; i < 100; i++ {
			fid2 := mdb.TimeTravelReverseId(t)
			So(fid1, ShouldEqual, fid2)
		}
	})
}
