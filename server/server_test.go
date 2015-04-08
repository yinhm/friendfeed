package server

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	uuid "github.com/satori/go.uuid"
	. "github.com/smartystreets/goconvey/convey"
	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
	"golang.org/x/net/context"
)

var (
	srv    *ApiServer
	dbpath string
	mcFile string
	job    *pb.FeedJob
)

func setup() {
	dbpath = os.TempDir() + "/fftestdb"
	mcFile = "../conf/media.json"

	srv = NewApiServer(dbpath, mcFile)

	job = &pb.FeedJob{
		Id:        "foobar",
		RemoteKey: "pwd",
		Start:     0,
		PageSize:  100,
		Created:   time.Now().Unix(),
		Updated:   time.Now().Unix(),
	}
}

func teardown() {
	rdb := *srv.rdb
	mdb := *srv.mdb
	srv.Shutdown()

	if err := rdb.Destroy(); err != nil {
		log.Fatalf("fail on destroy: %s", err)
	}
	if err := mdb.Destroy(); err != nil {
		log.Fatalf("fail on destroy: %s", err)
	}
}

func TestServerJob(t *testing.T) {
	setup()
	defer teardown()

	Convey("Manual enqueue to database", t, func() {
		key := store.NewFlakeKey(store.TableJobFeed, srv.mdb.NextId())

		job.Key = key.String()

		bytes, err := proto.Marshal(job)
		So(err, ShouldBeNil)

		err = srv.mdb.Put(key.Bytes(), bytes)
		So(err, ShouldBeNil)

		bytes, err = srv.mdb.Get(key.Bytes())
		So(err, ShouldBeNil)

		err = proto.Unmarshal(bytes, job)
		So(err, ShouldBeNil)

		key = store.NewFlakeKey(store.TableJobFeed, srv.mdb.NextId())
		// iter := srv.mdb.Iterator()
		// iter.Seek(key.Prefix().Bytes())
		// defer iter.Close()
		// So(iter.Valid(), ShouldBeTrue)

		got, err := srv.dequeJob()
		So(err, ShouldBeNil)
		So(got.Key, ShouldEqual, job.Key)
		So(got.Id, ShouldEqual, job.Id)
		So(got.RemoteKey, ShouldEqual, job.RemoteKey)

		got, err = srv.dequeJob()
		So(err, ShouldNotBeNil)
	})
}

func TestMdbReopen(t *testing.T) {
	setup()
	defer teardown()

	Convey("mdb reopen bug: Corruption on wrong key size", t, func() {
		key := store.NewFlakeKey(store.TableJobFeed, srv.mdb.NextId())
		job.Key = key.String()

		bytes, err := proto.Marshal(job)
		err = srv.mdb.Put(key.Bytes(), bytes)
		So(err, ShouldBeNil)

		// reopen to check data
		srv.Shutdown()
		srv = NewApiServer(dbpath, mcFile)

		got, err := srv.dequeJob()
		So(err, ShouldBeNil)
		So(got.Key, ShouldEqual, job.Key)
		So(got.Id, ShouldEqual, job.Id)
		So(got.RemoteKey, ShouldEqual, job.RemoteKey)
	})
}

func TestReopenDeque(t *testing.T) {
	setup()
	defer teardown()

	Convey("mdb redeque", t, func() {
		key := store.NewFlakeKey(store.TableJobFeed, srv.mdb.NextId())

		job.Key = key.String()
		mdb := srv.mdb

		bytes, err := proto.Marshal(job)
		err = mdb.Put(key.Bytes(), bytes)

		_, err = srv.dequeJob()
		So(err, ShouldBeNil)

		// reopen to check data
		srv.Shutdown()
		srv = NewApiServer(dbpath, mcFile)

		_, err = srv.dequeJob()
		So(err, ShouldNotBeNil)
	})
}

func TestJobQueue(t *testing.T) {
	setup()
	defer teardown()

	Convey("Given ApiServer, When enqueue job, should deque the same job", t, func() {
		ctx := context.Background()
		key := store.NewFlakeKey(store.TableJobFeed, srv.mdb.NextId())
		job.Key = key.String()

		srv.EnqueJob(ctx, job)
		jobs, err := srv.ListJobQueue(store.TableJobFeed)
		So(err, ShouldBeNil)
		So(len(jobs), ShouldEqual, 1)

		worker := &pb.Worker{
			Id: "123456",
		}
		got, err := srv.GetFeedJob(ctx, worker)
		So(err, ShouldBeNil)
		So(got.Id, ShouldEqual, job.Id)
		So(got.RemoteKey, ShouldEqual, job.RemoteKey)

		_, err = srv.dequeJob()
		So(err, ShouldNotBeNil)

		jobs, err = srv.ListJobQueue(store.TableJobRunning)
		So(err, ShouldBeNil)
		So(len(jobs), ShouldEqual, 1)

		// reopen to check data
		Convey("reopen db should got the same result: no job available", func() {
			srv.Shutdown()
			srv = NewApiServer(dbpath, mcFile)

			_, err = srv.dequeJob()
			So(err, ShouldNotBeNil)
		})
	})
}

func TestPurgeJobQueue(t *testing.T) {
	setup()
	defer teardown()

	Convey("Given job queue, do purge", t, func() {
		ctx := context.Background()
		key := store.NewFlakeKey(store.TableJobFeed, srv.mdb.NextId())
		job.Key = key.String()

		srv.EnqueJob(ctx, job)
		srv.ListJobQueue(store.TableJobFeed)

		cmd := &pb.CommandRequest{
			Command: "PurgeJobs",
		}
		srv.Command(ctx, cmd)
		jobs, err := srv.ListJobQueue(store.TableJobRunning)
		So(err, ShouldBeNil)
		So(len(jobs), ShouldEqual, 0)

		srv.EnqueJob(ctx, job)
		worker := &pb.Worker{
			Id: "123456",
		}
		srv.GetFeedJob(ctx, worker)
		srv.dequeJob()

		srv.Command(ctx, cmd)

		jobs, err = srv.ListJobQueue(store.TableJobRunning)
		So(err, ShouldBeNil)
		So(len(jobs), ShouldEqual, 0)
	})
}

func TestFinishJobQueue(t *testing.T) {
	setup()
	defer teardown()

	Convey("Given ApiServer, enqueue job, deque job, finish job", t, func() {
		ctx := context.Background()
		key := store.NewFlakeKey(store.TableJobFeed, srv.mdb.NextId())
		job.Key = key.String()
		job.TargetId = "targetId"

		srv.EnqueJob(ctx, job)

		worker := &pb.Worker{
			Id: "123456",
		}

		// running job
		newjob, err := srv.GetFeedJob(ctx, worker)
		So(err, ShouldBeNil)
		So(newjob.Id, ShouldEqual, job.Id)
		So(newjob.RemoteKey, ShouldEqual, job.RemoteKey)
		So(newjob.Key, ShouldNotEqual, job.Key)

		// finished job
		key1 := job.Key
		newjob.Id = "name"
		newjob.Uuid = "c6f8dca854f011ddb489003048343a40"
		finjob, err := srv.FinishJob(ctx, newjob)
		So(err, ShouldBeNil)
		So(finjob.Key, ShouldNotEqual, key1)
		So(finjob.Status, ShouldEqual, "done")

		dbjob, err := store.GetArchiveHistory(srv.mdb, newjob.TargetId)
		So(err, ShouldBeNil)
		So(finjob.Key, ShouldEqual, dbjob.Key)
		So(dbjob.Status, ShouldEqual, "done")

		// check running job states
		jobs, err := srv.ListJobQueue(store.TableJobRunning)
		So(err, ShouldBeNil)
		So(len(jobs), ShouldEqual, 0)
	})
}

func TestPostProfile(t *testing.T) {
	setup()
	defer teardown()

	Convey("Given ApiServer, post profile then get profile", t, func() {
		ctx := context.Background()

		p1 := &pb.Profile{
			Uuid:        "c6f8dca854f011ddb489003048343a40",
			Id:          "yinhm",
			Name:        "yinhm",
			Type:        "user",
			Private:     false,
			SupId:       "4566789",
			Description: "desc",
			RemoteKey:   "xxx",
		}

		feedinfo := &pb.Feedinfo{
			Uuid:          "c6f8dca854f011ddb489003048343a40",
			Id:            "yinhm",
			Name:          "Heming Friend",
			Type:          "user",
			Private:       false,
			SupId:         "123456-1234",
			Description:   "Friendfeed land",
			Subscriptions: []*pb.Profile{p1},
		}
		got, err := srv.PostFeedinfo(ctx, feedinfo)
		So(err, ShouldBeNil)
		So(got.Uuid, ShouldEqual, feedinfo.Uuid)
		So(got.RemoteKey, ShouldEqual, feedinfo.RemoteKey)

		profile, err := store.GetProfile(srv.mdb, feedinfo.Id)
		So(err, ShouldBeNil)
		So(profile.Uuid, ShouldEqual, feedinfo.Uuid)
		So(profile.RemoteKey, ShouldEqual, feedinfo.RemoteKey)

		newinfo, err := store.GetFeedinfo(srv.rdb, feedinfo.Uuid)
		So(err, ShouldBeNil)
		So(newinfo.Uuid, ShouldEqual, feedinfo.Uuid)
		So(newinfo.RemoteKey, ShouldEqual, feedinfo.RemoteKey)
		So(len(newinfo.Subscriptions), ShouldEqual, 1)

		Convey("post entry", func() {
			from := &pb.Feed{
				Id:   "yinhm",
				Name: "yinhm",
				Type: "user",
			}
			entry := &pb.Entry{
				Body:        "张无忌对张三丰说：“太师父，武当山的生活太寂寞了，只有清风和明月两个朋友能陪我玩。”张三丰叹了口气：“已经很不错啦，至少还有清风明月呢。想当年我在少林寺的时候，也是只有两个朋友，其中一个也叫清风……”“那另一个呢？”“叫心相印。”…",
				Id:          "e/2b43a9066074d120ed2e45494eea1797",
				Date:        "2012-09-07T07:40:22Z",
				Url:         "http://friendfeed.com/yinhm/2b43a906/rt-trojansj",
				From:        from,
				ProfileUuid: "c6f8dca854f011ddb489003048343a40",
			}

			_, err := store.PutEntry(srv.rdb, entry, false)
			So(err, ShouldBeNil)

			req := &pb.FeedRequest{
				Id:       "yinhm",
				Start:    0,
				PageSize: 50,
			}
			feed, err := srv.FetchFeed(context.Background(), req)
			So(err, ShouldBeNil)
			So(feed.Id, ShouldEqual, "yinhm")
			So(len(feed.Entries), ShouldEqual, 1)
			// prefix stripped
			So(feed.Entries[0].Id, ShouldEqual, "2b43a9066074d120ed2e45494eea1797")
		})
	})
}

func TestFeedIndexLoadDump(t *testing.T) {
	setup()
	defer teardown()

	Convey("Given FeedIndex, load and dump to db", t, func() {
		uuid1 := "c6f8dca854f011ddb489003048343a40"
		index := NewFeedIndex("public", new(uuid.UUID))
		err := index.load(srv.mdb)
		So(err, ShouldBeNil)

		for i := 0; i < 10; i++ {
			// index.itemCh <- uuid
			index.Push(uuid1)
		}

		index.rebuild()
		So(index.bufq[0], ShouldEqual, "c6f8dca854f011ddb489003048343a40")
		index.bufq[len(index.bufq)-1] = "last"

		err = index.dump(srv.mdb)
		So(err, ShouldBeNil)

		err = index.load(srv.mdb)
		So(err, ShouldBeNil)
		So(index.bufq[0], ShouldEqual, "c6f8dca854f011ddb489003048343a40")

		for i := 1; i < len(index.bufq)-1; i++ {
			So(index.bufq[i], ShouldEqual, "")
		}
		So(index.bufq[len(index.bufq)-1], ShouldEqual, "last")
	})
}
