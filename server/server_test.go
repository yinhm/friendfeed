package server

import (
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type RpcTestSuite struct {
	suite.Suite

	srv       *ApiServer
	rpcServer *grpc.Server

	dbpath string
	mcFile string
	job    *pb.FeedJob
}

func TestRpcTestSuite(t *testing.T) {
	suite.Run(t, new(RpcTestSuite))
}

func (s *RpcTestSuite) SetupTest() {
	log.Println("setup tests...")
	s.dbpath = os.TempDir() + "/fftestdb"
	s.mcFile = "../conf/example.config.json"

	search.InitMockIndexService(filepath.Join(s.dbpath, "index"))

	s.job = &pb.FeedJob{
		Id:       "foobar",
		Start:    0,
		PageSize: 100,
		Created:  time.Now().Unix(),
		Updated:  time.Now().Unix(),
	}

	ln, err := net.Listen("tcp", ":12019")
	if err != nil {
		log.Fatalf("Can not bind : %s\n", err)
	}

	s.rpcServer = grpc.NewServer()
	s.srv = NewApiServer(s.dbpath, s.mcFile)

	pb.RegisterApiServer(s.rpcServer, s.srv)
	go s.rpcServer.Serve(ln)
}

func (s *RpcTestSuite) TearDownTest() {
	log.Println("teardown tests...")

	s.srv.Shutdown()
	s.rpcServer.Stop()
	os.RemoveAll(s.dbpath)
}

func (s *RpcTestSuite) TestServerJob() {
	// Manual enqueue to database
	key := store.NewFlakeKey(model.TableJobFeed, s.srv.mdb.NextId())

	s.job.Key = key.String()

	bytes, err := proto.Marshal(s.job)
	assert.Nil(s.T(), err)

	err = s.srv.mdb.Put(key.Bytes(), bytes)
	assert.Nil(s.T(), err)

	bytes, err = s.srv.mdb.Get(key.Bytes())
	assert.Nil(s.T(), err)

	err = proto.Unmarshal(bytes, s.job)
	assert.Nil(s.T(), err)

	// key = store.NewFlakeKey(model.TableJobFeed, s.srv.mdb.NextId())
	// iter := s.srv.mdb.Iterator()
	// iter.Seek(key.Prefix().Bytes())
	// defer iter.Close()
	// assert.True(s.T(), iter.Valid())

	got, err := s.srv.dequeJob()
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), got.Key, s.job.Key)
	assert.Equal(s.T(), got.Id, s.job.Id)

	_, err = s.srv.dequeJob()
	assert.NotNil(s.T(), err)
}

func (s *RpcTestSuite) TestMdbReopen() {
	// mdb reopen bug: Corruption on wrong key size
	key := store.NewFlakeKey(model.TableJobFeed, s.srv.mdb.NextId())
	s.job.Key = key.String()

	bytes, err := proto.Marshal(s.job)
	assert.Nil(s.T(), err)
	err = s.srv.mdb.Put(key.Bytes(), bytes)
	assert.Nil(s.T(), err)

	// reopen to check data
	s.srv.Shutdown()
	s.srv = NewApiServer(s.dbpath, s.mcFile)

	got, err := s.srv.dequeJob()
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), got.Key, s.job.Key)
	assert.Equal(s.T(), got.Id, s.job.Id)
}

func (s *RpcTestSuite) TestReopenDeque() {
	// mdb redeque
	key := store.NewFlakeKey(model.TableJobFeed, s.srv.mdb.NextId())

	s.job.Key = key.String()
	mdb := s.srv.mdb

	bytes, err := proto.Marshal(s.job)
	assert.Nil(s.T(), err)
	err = mdb.Put(key.Bytes(), bytes)
	assert.Nil(s.T(), err)

	_, err = s.srv.dequeJob()
	assert.Nil(s.T(), err)

	// reopen to check data
	s.rpcServer.Stop()
	s.srv.Shutdown()
	s.srv = NewApiServer(s.dbpath, s.mcFile)

	_, err = s.srv.dequeJob()
	assert.NotNil(s.T(), err)
}

func (s *RpcTestSuite) TestJobQueue() {
	// Given ApiServer, When enqueue job, should deque the same job
	ctx := context.Background()
	key := store.NewFlakeKey(model.TableJobFeed, s.srv.mdb.NextId())
	s.job.Key = key.String()

	s.srv.EnqueJob(ctx, s.job)
	jobs, err := s.srv.ListJobQueue(model.TableJobFeed)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), len(jobs), 1)

	worker := &pb.Worker{
		Id: "123456",
	}
	got, err := s.srv.GetFeedJob(ctx, worker)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), got.Id, s.job.Id)

	_, err = s.srv.dequeJob()
	assert.NotNil(s.T(), err)

	jobs, err = s.srv.ListJobQueue(model.TableJobRunning)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), len(jobs), 1)

	// reopen to check data
	// reopen db should got the same result: no job available
	s.srv.Shutdown()
	s.srv = NewApiServer(s.dbpath, s.mcFile)

	_, err = s.srv.dequeJob()
	assert.NotNil(s.T(), err)
}

func (s *RpcTestSuite) TestPurgeJobQueue() {
	// Given job queue, do purge
	ctx := context.Background()
	key := store.NewFlakeKey(model.TableJobFeed, s.srv.mdb.NextId())
	s.job.Key = key.String()

	s.srv.EnqueJob(ctx, s.job)
	s.srv.ListJobQueue(model.TableJobFeed)

	cmd := &pb.CommandRequest{
		Command: "PurgeJobs",
	}
	s.srv.Command(ctx, cmd)
	jobs, err := s.srv.ListJobQueue(model.TableJobRunning)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), len(jobs), 0)

	s.srv.EnqueJob(ctx, s.job)
	worker := &pb.Worker{
		Id: "123456",
	}
	s.srv.GetFeedJob(ctx, worker)
	s.srv.dequeJob()

	s.srv.Command(ctx, cmd)

	jobs, err = s.srv.ListJobQueue(model.TableJobRunning)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), len(jobs), 0)
}

func (s *RpcTestSuite) TestFinishJobQueue() {
	// Given ApiServer, enqueue job, deque job, finish job
	ctx := context.Background()
	key := store.NewFlakeKey(model.TableJobFeed, s.srv.mdb.NextId())
	s.job.Key = key.String()
	s.job.TargetId = "targetId"

	s.srv.EnqueJob(ctx, s.job)

	worker := &pb.Worker{
		Id: "123456",
	}

	// running job
	newjob, err := s.srv.GetFeedJob(ctx, worker)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), newjob.Id, s.job.Id)
	assert.NotEqual(s.T(), newjob.Key, s.job.Key)

	// finished job
	key1 := s.job.Key
	newjob.Id = "name"
	newjob.Uuid = "c6f8dca854f011ddb489003048343a40"
	finjob, err := s.srv.FinishJob(ctx, newjob)
	assert.Nil(s.T(), err)
	assert.NotEqual(s.T(), finjob.Key, key1)
	assert.Equal(s.T(), finjob.Status, "done")

	dbjob, err := model.GetArchiveHistory(s.srv.mdb, newjob.TargetId)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), finjob.Key, dbjob.Key)
	assert.Equal(s.T(), dbjob.Status, "done")

	// check running job states
	jobs, err := s.srv.ListJobQueue(model.TableJobRunning)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), len(jobs), 0)
}

func (s *RpcTestSuite) TestPostProfile() {
	// Given ApiServer, post profile then get profile
	ctx := context.Background()

	p1 := &pb.Profile{
		Uuid:        "c6f8dca854f011ddb489003048343a40",
		Id:          "yinhm",
		Name:        "yinhm",
		Type:        "user",
		Private:     false,
		Description: "desc",
	}

	feedinfo := &pb.Feedinfo{
		Uuid:        "c6f8dca854f011ddb489003048343a40",
		Id:          "yinhm",
		Name:        "Heming Friend",
		Type:        "user",
		Private:     false,
		Description: "Friendfeed land",
		Following:   []*pb.Profile{p1},
	}
	got, err := s.srv.PostFeedinfo(ctx, feedinfo)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), got.Uuid, feedinfo.Uuid)

	profile, err := model.GetProfileFromUserId(s.srv.mdb, feedinfo.Id)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), profile.Uuid, feedinfo.Uuid)

	// we are not saving feedinfo to model
	// newinfo, err := model.GetFeedinfo(s.srv.rdb, feedinfo.Uuid)
	// assert.Nil(s.T(), err)
	// assert.Equal(s.T(), newinfo.Uuid, feedinfo.Uuid)
	// assert.Equal(s.T(), len(newinfo.Subscriptions), 1)

	// post entry
	from := &pb.Feed{
		Id:   "yinhm",
		Name: "yinhm",
		Type: "user",
	}
	entry := &pb.Entry{
		Body:        "张无忌对张三丰说：“太师父，武当山的生活太寂寞了，只有清风和明月两个朋友能陪我玩。”张三丰叹了口气：“已经很不错啦，至少还有清风明月呢。想当年我在少林寺的时候，也是只有两个朋友，其中一个也叫清风……”“那另一个呢？”“叫心相印。”…",
		Id:          "2b43a9066074d120ed2e45494eea1797",
		Date:        "2012-09-07T07:40:22Z",
		Url:         "http://friendfeed.com/yinhm/2b43a906/rt-trojansj",
		From:        from,
		ProfileUuid: "c6f8dca854f011ddb489003048343a40",
	}

	_, err = model.PutEntry(s.srv.rdb, entry)
	assert.Nil(s.T(), err)

	req := &pb.FeedRequest{
		Id:       "yinhm",
		Start:    0,
		PageSize: 50,
	}
	feed, err := s.srv.FetchFeed(context.Background(), req)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), feed.Id, "yinhm")
	assert.Equal(s.T(), len(feed.Entries), 1)
	// prefix stripped
	assert.Equal(s.T(), feed.Entries[0].Id, "2b43a9066074d120ed2e45494eea1797")

	// fetch user timeline
	tReq := &pb.FeedRequest{
		Id:          "yinhm",
		ProfileUuid: "c6f8dca854f011ddb489003048343a40",
	}
	feed, err = s.srv.FetchFeed(context.Background(), tReq)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), feed.Id, "yinhm")
	assert.Equal(s.T(), len(feed.Entries), 1)

	// comment on the entry
	cmt := &pb.Comment{
		Id:      "2b43a9066074d120ed2e45494eea1797",
		Date:    "2012-09-07T07:40:22Z",
		From:    from,
		Body:    "this is a comment",
		RawBody: "this is a comment",
	}
	cReq := &pb.CommentRequest{
		Entry:   entry.Id,
		Comment: cmt,
	}
	entry, err = s.srv.CommentEntry(context.Background(), cReq)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), entry.Id, "2b43a9066074d120ed2e45494eea1797")

	// BUG: duplicated entry when comment
	feed, err = s.srv.FetchFeed(context.Background(), req)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), feed.Id, "yinhm")
	assert.Equal(s.T(), len(feed.Entries), 1)

	// delete entry
	dReq := &pb.EntryRequest{
		Uuid: entry.Id,
		User: entry.ProfileUuid,
	}
	_, err = s.srv.DeleteEntry(context.Background(), dReq)
	assert.Nil(s.T(), err)

	// refetch feed
	feed, err = s.srv.FetchFeed(context.Background(), req)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), feed.Id, "yinhm")
	assert.Equal(s.T(), len(feed.Entries), 0)

	p2 := &pb.Profile{
		Uuid:        "4e580875-46c3-58fe-a436-bcc17d7e2509",
		Id:          "foobar",
		Name:        "foobar",
		Type:        "user",
		Private:     false,
		Description: "desc",
	}
	feedinfo2 := model.ProfileToFeedinfo(p2)
	got, err = s.srv.PostFeedinfo(ctx, feedinfo2)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), got.Uuid, feedinfo2.Uuid)

	// p2 follow p1
	fReq := &pb.FollowRequest{
		ProfileUuid: p2.Uuid,
		FeedUuid:    p1.Uuid,
		Action:      "follow",
	}
	_, err = s.srv.GraphFollow(ctx, fReq)
	assert.Nil(s.T(), err)

	// post entry to p1 now fanout to p2 user timeline
	_, err = model.PutEntry(s.srv.rdb, entry)
	assert.Nil(s.T(), err)

	tReq = &pb.FeedRequest{
		Id:          "foobar",
		ProfileUuid: "4e580875-46c3-58fe-a436-bcc17d7e2509",
	}
	feed, err = s.srv.FetchFeed(context.Background(), tReq)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "foobar", feed.Id)
	assert.Equal(s.T(), 1, len(feed.Entries))

	// delete fanout aswell
	_, err = s.srv.DeleteEntry(context.Background(), dReq)
	assert.Nil(s.T(), err)
	feed, err = s.srv.FetchFeed(context.Background(), tReq)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 0, len(feed.Entries))
}

func (s *RpcTestSuite) TestFeedIndexLoadDump() {
	// Given FeedIndex, load and dump to db
	uuid1 := "c6f8dca854f011ddb489003048343a40"
	index := NewFeedIndex(nil, "public", uuid.Must(uuid.NewV4()))
	err := index.load(s.srv.mdb)
	assert.Nil(s.T(), err)

	for i := 0; i < 10; i++ {
		// index.itemCh <- uuid
		index.Push(uuid1)
	}

	index.rebuild(nil)
	assert.Equal(s.T(), index.bufq[0], "c6f8dca854f011ddb489003048343a40")
	index.bufq[len(index.bufq)-1] = "last"

	err = index.dump(s.srv.mdb)
	assert.Nil(s.T(), err)

	err = index.load(s.srv.mdb)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), index.bufq[0], "c6f8dca854f011ddb489003048343a40")

	for i := 1; i < len(index.bufq)-1; i++ {
		assert.Equal(s.T(), index.bufq[i], "")
	}
	assert.Equal(s.T(), index.bufq[len(index.bufq)-1], "last")
}

func (s *RpcTestSuite) TestNewProfileThenPostEntry() {
	ctx := context.Background()

	req := &pb.OAuthUser{
		Uuid:              "",
		Name:              "demo",
		NickName:          "demouser",
		UserId:            "6666666",
		AccessToken:       "",
		AccessTokenSecret: "",
		Provider:          "Twitter",
	}
	profile, err := s.srv.PutOAuth(ctx, req)
	assert.Nil(s.T(), err)
	assert.NotEmpty(s.T(), profile.Uuid)

	from := &pb.Feed{
		Id:   "demo",
		Name: "yinhm",
		Type: "user",
	}

	entry := &pb.Entry{
		Body:        "张无忌对张三丰说：“太师父，武当山的生活太寂寞了，只有清风和明月两个朋友能陪我玩。”张三丰叹了口气：“已经很不错啦，至少还有清风明月呢。想当年我在少林寺的时候，也是只有两个朋友，其中一个也叫清风……”“那另一个呢？”“叫心相印。”…",
		Date:        "2012-09-07T07:40:22Z",
		Url:         "http://friendfeed.com/yinhm/2b43a906/rt-trojansj",
		From:        from,
		ProfileUuid: profile.Uuid,
	}

	// new entry id
	dt := time.Now().UTC()
	name := profile.Uuid + "/" + dt.Format(time.RFC3339)
	uuid1 := uuid.NewV5(uuid.NamespaceURL, name)
	entry.Id = uuid1.String()

	entry, err = s.srv.PostEntry(context.Background(), entry)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), len(entry.Id), 36)

	entryReq := &pb.EntryRequest{Uuid: entry.Id}
	feed, err := s.srv.FetchEntry(context.Background(), entryReq)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), feed.Entries[0].Id, entry.Id)

	// Feed
	// Feedinfo never saved to db
	// _, err = model.GetFeedinfo(s.srv.rdb, profile.Uuid)
	// assert.Nil(s.T(), err)

	// delete service
	dReq := &pb.ServiceRequest{
		User:    profile.Uuid,
		Service: "twitter",
	}
	_, err = s.srv.DeleteService(context.Background(), dReq)
	assert.Nil(s.T(), err)

	// FetchGraph panic
	// panic: runtime error: invalid memory address or nil pointer dereference
	gReq := &pb.ProfileRequest{
		Uuid: profile.Uuid,
	}
	_, err = s.srv.FetchGraph(context.Background(), gReq)
	assert.Nil(s.T(), err)
}

func (s *RpcTestSuite) TestKLines() {
	conn, err := grpc.Dial("localhost:12019", grpc.WithInsecure())
	assert.Nil(s.T(), err)
	defer conn.Close()

	api := pb.NewApiClient(conn)
	ctx := context.Background()

	// KLines
	stream, err := api.ArchiveKLine(ctx)
	assert.Nil(s.T(), err)

	dt := time.Date(2021, 7, 25, 15, 0, 0, 0, time.UTC)

	kp := &pb.KLineRequest{
		Symbol: "600519.SH",
		KLine: &pb.KLine{
			Date:   int32(dt.Unix()),
			Open:   1912.0,
			High:   8848.0,
			Low:    1234.0,
			Close:  2330.0,
			Volume: 10240.0,
			Amount: 102400.0,
		},
	}

	err = stream.Send(kp)
	assert.Nil(s.T(), err)

	resp, err := stream.CloseAndRecv()
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), int32(1), resp.Count)

	// now scan key
	// 0000012f093cc15911635c5c822f0b31db5089f8000006aee6c8de801c697aa5a6ca0000
	// prefix from
	// 0000012f093cc15911635c5c822f0b31db5089f8

	// KLines
	req := &pb.StockRequest{
		Symbol: "600519.SH",
		Bars:   5,
	}
	klineResp, err := api.GetKLines(ctx, req)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 1, len(klineResp.KLines))
	kline := klineResp.KLines[0]
	assert.EqualValues(s.T(), 8848.0, kline.High)

	// ListStock
	reqStockList := &pb.StockList{
		Stocks: []*pb.Stock{{
			Symbol: "600519.SH",
		}},
	}
	_, err = api.UpdateStockList(ctx, reqStockList)
	assert.Nil(s.T(), err)

	req = &pb.StockRequest{
		Symbol: "",
	}
	respStockList, err := api.GetStockList(ctx, req)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 1, len(respStockList.Stocks))

	// archive xrxd
	streamXrxd, err := api.ArchiveXRXD(ctx)
	assert.Nil(s.T(), err)

	dt = time.Date(2021, 7, 25, 0, 0, 0, 0, time.UTC)
	msg := &pb.XRXD{
		Symbol:        "600519.SH",
		ExDate:        int32(dt.Unix()), // ex date?
		Dividend:      101.0,
		PurchasePrice: 0,
		Split:         0,
		Purchase:      0,
	}
	err = streamXrxd.Send(msg)
	assert.Nil(s.T(), err)

	resp, err = streamXrxd.CloseAndRecv()
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), int32(1), resp.Count)

	// get xrxd
	req = &pb.StockRequest{
		Symbol: "600519.SH",
	}
	xrxds, err := api.GetXRXD(ctx, req)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 1, len(xrxds.XRXDS))
}
