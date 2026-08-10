package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/store/flake"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type RpcTestSuite struct {
	suite.Suite

	srv       *ApiServer
	rpcServer *grpc.Server

	dbpath     string
	rpcAddress string
	cfg        *util.Config
	job        *pb.FeedJob
}

func TestRpcTestSuite(t *testing.T) {
	suite.Run(t, new(RpcTestSuite))
}

func (s *RpcTestSuite) SetupTest() {
	log.Println("setup tests...")
	s.dbpath = s.T().TempDir()
	cfg, _ := util.NewConfigFromJSON("../conf/example.config.json")
	s.cfg = cfg

	search.InitMockIndexService(filepath.Join(s.dbpath, "index"))

	s.job = &pb.FeedJob{
		Id:       "foobar",
		Start:    0,
		PageSize: 100,
		Created:  time.Now().Unix(),
		Updated:  time.Now().Unix(),
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	s.rpcAddress = ln.Addr().String()

	s.rpcServer = grpc.NewServer()
	srv, err := NewApiServer(s.dbpath, cfg)
	s.Require().NoError(err)
	s.srv = srv

	pb.RegisterApiServer(s.rpcServer, s.srv)
	go s.rpcServer.Serve(ln)
}

func (s *RpcTestSuite) TearDownTest() {
	log.Println("teardown tests...")

	s.srv.Shutdown()
	s.rpcServer.Stop()
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

func (s *RpcTestSuite) TestFetchFeedResolvesPreviousProfileID() {
	profileUUID := uuid.Must(uuid.NewV4())
	if err := model.UpdateProfile(s.srv.mdb, &pb.Profile{
		Uuid: profileUUID.String(),
		Id:   "before-rename",
		Name: "Renamed User",
		Type: "user",
	}); err != nil {
		s.T().Fatal(err)
	}
	if err := model.RenameProfileId(s.srv.mdb, profileUUID, "after-rename"); err != nil {
		s.T().Fatal(err)
	}

	feed, err := s.srv.FetchFeed(context.Background(), &pb.FeedRequest{
		Id:       "before-rename",
		PageSize: 30,
	})
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "after-rename", feed.Id)
	assert.Equal(s.T(), profileUUID.String(), feed.Uuid)
}

func (s *RpcTestSuite) TestCursorFeedPagesForwardAndSurvivesDeletedAnchor() {
	profileUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{Uuid: profileUUID.String(), Id: "cursor-user", Name: "Cursor User", Type: "user"}
	s.Require().NoError(model.UpdateProfile(s.srv.mdb, profile))

	entryIDs := make([]string, 5)
	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	for i := range entryIDs {
		entryID := uuid.NewV5(uuid.NamespaceURL, fmt.Sprintf("cursor-entry-%d", i)).String()
		entryIDs[i] = entryID
		_, err := model.PutEntry(s.srv.rdb, &pb.Entry{
			Id:          entryID,
			Date:        base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
			Body:        fmt.Sprintf("entry %d", i),
			RawBody:     fmt.Sprintf("entry %d", i),
			ProfileUuid: profileUUID.String(),
			From:        &pb.Feed{Uuid: profileUUID.String(), Id: profile.Id, Name: profile.Name, Type: profile.Type},
		})
		s.Require().NoError(err)
	}
	legacy, err := s.srv.FetchFeed(context.Background(), &pb.FeedRequest{
		Id: profile.Id, PageSize: 2,
	})
	s.Require().NoError(err)
	s.Len(legacy.Entries, 3, "legacy pagination should return one lookahead entry")

	first, err := s.srv.FetchFeed(context.Background(), &pb.FeedRequest{
		Id: profile.Id, PageSize: 2, CursorPaging: true,
	})
	s.Require().NoError(err)
	s.Equal([]string{entryIDs[4], entryIDs[3]}, feedEntryIDs(first))
	s.NotEmpty(first.NextCursor)

	// A raw index cursor remains usable even when its anchor entry and index
	// row disappear between requests.
	s.Require().NoError(model.DeleteEntry(s.srv.rdb, entryIDs[3]))
	second, err := s.srv.FetchFeed(context.Background(), &pb.FeedRequest{
		Id: profile.Id, PageSize: 2, CursorPaging: true, Cursor: first.NextCursor,
	})
	s.Require().NoError(err)
	s.Equal([]string{entryIDs[2], entryIDs[1]}, feedEntryIDs(second))
	s.NotEmpty(second.NextCursor)

	third, err := s.srv.FetchFeed(context.Background(), &pb.FeedRequest{
		Id: profile.Id, PageSize: 2, CursorPaging: true, Cursor: second.NextCursor,
	})
	s.Require().NoError(err)
	s.Equal([]string{entryIDs[0]}, feedEntryIDs(third))
	s.Empty(third.NextCursor)
}

func (s *RpcTestSuite) TestFeedCursorOmitsFixedIndexPrefix() {
	prefix := store.NewUUIDKey(model.TableEntryIndex, uuid.Must(uuid.NewV4())).Bytes()
	var position flake.Id
	for i := range position {
		position[i] = byte(i + 1)
	}
	key := append(append(store.Key(nil), prefix...), position[:]...)

	cursor := encodeFeedCursor(key, prefix)
	encoded, err := base64.RawURLEncoding.DecodeString(cursor)
	s.Require().NoError(err)
	s.Equal(position[:], encoded)

	decoded, err := decodeFeedCursor(cursor, prefix)
	s.Require().NoError(err)
	s.Equal(key, decoded)

	_, err = decodeFeedCursor(base64.RawURLEncoding.EncodeToString(position[:len(position)-1]), prefix)
	s.Error(err)
}

func feedEntryIDs(feed *pb.Feed) []string {
	ids := make([]string, len(feed.Entries))
	for i := range feed.Entries {
		ids[i] = feed.Entries[i].Id
	}
	return ids
}

func (s *RpcTestSuite) TestRedoFailedJob() {
	// Simulate a job pulled by a worker that never finished: it lives in
	// TableJobRunning and is gone from the queue.
	_, err := s.srv.EnqueJob(context.Background(), s.job)
	assert.Nil(s.T(), err)
	_, err = s.srv.GetFeedJob(context.Background(), &pb.Worker{Id: "test-worker"})
	assert.Nil(s.T(), err)

	// Queue is drained, one running job remains.
	_, err = s.srv.dequeJob()
	assert.NotNil(s.T(), err)

	err = s.srv.RedoFailedJob()
	assert.Nil(s.T(), err)

	// The running record is deleted and the job is back in the queue.
	running, err := s.srv.ListJobQueue(model.TableJobRunning)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 0, len(running))

	got, err := s.srv.dequeJob()
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), s.job.Id, got.Id)

	// A second redo with an empty running table must not enqueue a duplicate.
	err = s.srv.RedoFailedJob()
	assert.Nil(s.T(), err)
	_, err = s.srv.dequeJob()
	assert.NotNil(s.T(), err)
}

func (s *RpcTestSuite) TestEnqueJobReportsError() {
	// EnqueJob must report failures instead of swallowing them: it used to
	// ignore the mdb.Put error and report success, which made RedoFailedJob
	// drop the running record. The failure is injected at proto.Marshal
	// because pebble panics (rather than errors) on a closed instance.
	_, err := s.srv.EnqueJob(context.Background(), &pb.FeedJob{Id: "\xff"})
	assert.NotNil(s.T(), err)
}

func (s *RpcTestSuite) TestGetFeedJobMarshalFailureKeepsQueuedJob() {
	ctx := context.Background()
	queued, err := s.srv.EnqueJob(ctx, &pb.FeedJob{Id: "keep-me"})
	s.Require().NoError(err)
	queuedKey := queued.Key

	// Invalid UTF-8 in a protobuf string makes marshaling the running record
	// fail. The queued record must remain claimable.
	_, err = s.srv.GetFeedJob(ctx, &pb.Worker{Id: "\xff"})
	s.Require().Error(err)

	jobs, err := s.srv.ListJobQueue(model.TableJobFeed)
	s.Require().NoError(err)
	s.Require().Len(jobs, 1)
	s.Equal("keep-me", jobs[0].Id)
	s.Equal(queuedKey, jobs[0].Key)

	running, err := s.srv.ListJobQueue(model.TableJobRunning)
	s.Require().NoError(err)
	s.Empty(running)
}

func (s *RpcTestSuite) TestGetFeedJobConcurrentClaimIsUnique() {
	ctx := context.Background()
	_, err := s.srv.EnqueJob(ctx, &pb.FeedJob{Id: "only-job"})
	s.Require().NoError(err)

	const consumers = 2
	results := make(chan *pb.FeedJob, consumers)
	errs := make(chan error, consumers)
	var wg sync.WaitGroup
	wg.Add(consumers)
	for i := 0; i < consumers; i++ {
		go func(workerID string) {
			defer wg.Done()
			job, err := s.srv.GetFeedJob(ctx, &pb.Worker{Id: workerID})
			results <- job
			errs <- err
		}(fmt.Sprintf("worker-%d", i))
	}
	wg.Wait()
	close(results)
	close(errs)

	successes := 0
	failures := 0
	for err := range errs {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	s.Equal(1, successes)
	s.Equal(1, failures)

	claimed := 0
	for job := range results {
		if job != nil {
			claimed++
			s.Equal("only-job", job.Id)
		}
	}
	s.Equal(1, claimed)

	queued, err := s.srv.ListJobQueue(model.TableJobFeed)
	s.Require().NoError(err)
	s.Empty(queued)
	running, err := s.srv.ListJobQueue(model.TableJobRunning)
	s.Require().NoError(err)
	s.Require().Len(running, 1)
	s.Equal("only-job", running[0].Id)
}

func (s *RpcTestSuite) TestGetFeedJobPreservesQueueOrderAndRunningFields() {
	ctx := context.Background()
	first, err := s.srv.EnqueJob(ctx, &pb.FeedJob{Id: "first"})
	s.Require().NoError(err)
	firstQueuedKey := first.Key
	_, err = s.srv.EnqueJob(ctx, &pb.FeedJob{Id: "second"})
	s.Require().NoError(err)

	claimed, err := s.srv.GetFeedJob(ctx, &pb.Worker{Id: "worker-order"})
	s.Require().NoError(err)
	s.Equal("first", claimed.Id)
	s.Equal("worker-order", claimed.Worker)
	s.NotEqual(firstQueuedKey, claimed.Key)
	s.NotZero(claimed.Created)
	s.NotZero(claimed.Updated)

	next, err := s.srv.GetFeedJob(ctx, &pb.Worker{Id: "worker-order"})
	s.Require().NoError(err)
	s.Equal("second", next.Id)
}

func (s *RpcTestSuite) TestGetFeedJobCorruptQueuedRecordStaysQueued() {
	key := store.NewFlakeKey(model.TableJobFeed, s.srv.mdb.NextId())
	s.Require().NoError(s.srv.mdb.Put(key.Bytes(), []byte{0xff}))

	_, err := s.srv.GetFeedJob(context.Background(), &pb.Worker{Id: "worker"})
	s.Require().Error(err)

	raw, err := s.srv.mdb.Get(key.Bytes())
	s.Require().NoError(err)
	s.Equal([]byte{0xff}, raw)
	running, err := s.srv.ListJobQueue(model.TableJobRunning)
	s.Require().NoError(err)
	s.Empty(running)
}

func (s *RpcTestSuite) TestRedoFailedJobCommandError() {
	// A corrupt running record must surface through the Command RPC
	// instead of being reported as success.
	key := store.NewFlakeKey(model.TableJobRunning, s.srv.mdb.NextId())
	err := s.srv.mdb.Put(key.Bytes(), []byte{0xff})
	assert.Nil(s.T(), err)

	_, err = s.srv.Command(context.Background(), &pb.CommandRequest{Command: "RedoFailedJob"})
	assert.NotNil(s.T(), err)
}

func (s *RpcTestSuite) TestListJobQueueReportsDecodeError() {
	key := store.NewFlakeKey(model.TableJobRunning, s.srv.mdb.NextId())
	err := s.srv.mdb.Put(key.Bytes(), []byte{0xff})
	assert.Nil(s.T(), err)

	jobs, err := s.srv.ListJobQueue(model.TableJobRunning)
	assert.Nil(s.T(), jobs)
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
	srv, err := NewApiServer(s.dbpath, s.cfg)
	s.Require().NoError(err)
	s.srv = srv

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
	srv, err := NewApiServer(s.dbpath, s.cfg)
	s.Require().NoError(err)
	s.srv = srv

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
	srv, err := NewApiServer(s.dbpath, s.cfg)
	s.Require().NoError(err)
	s.srv = srv

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
		Entry:    entry.Id,
		Comment:  cmt,
		UserUuid: "c6f8dca854f011ddb489003048343a40",
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

// TestPostFeedinfoPreservesSystemFields guards the profile edit path:
// Feedinfo carries no is_super/deleted fields, so an update that rewrites
// the whole Profile record would silently strip them (e.g. a super admin
// editing only their display name would lose IsSuper).
func (s *RpcTestSuite) TestPostFeedinfoPreservesSystemFields() {
	ctx := context.Background()

	profileUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{
		Uuid:    profileUUID.String(),
		Id:      "superadmin",
		Name:    "Super Admin",
		Type:    "user",
		IsSuper: true,
	}
	assert.Nil(s.T(), model.UpdateProfile(s.srv.mdb, profile))

	// Edit the display name only, no ID change (the non-rename path).
	feedinfo := &pb.Feedinfo{
		Uuid: profileUUID.String(),
		Id:   "superadmin",
		Name: "Renamed Admin",
		Type: "user",
	}
	got, err := s.srv.PostFeedinfo(ctx, feedinfo)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "Renamed Admin", got.Name)
	assert.True(s.T(), got.IsSuper, "IsSuper must survive a plain profile edit")

	stored, err := model.GetProfileFromUuid(s.srv.mdb, profileUUID)
	assert.Nil(s.T(), err)
	assert.True(s.T(), stored.IsSuper, "stored profile must keep IsSuper")

	// Same guarantee on the rename path.
	feedinfo.Id = "superadmin2"
	got, err = s.srv.PostFeedinfo(ctx, feedinfo)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "superadmin2", got.Id)
	assert.True(s.T(), got.IsSuper, "IsSuper must survive a rename")
}

func (s *RpcTestSuite) TestPostFeedinfoSerializesConcurrentRenameCollision() {
	ctx := context.Background()
	firstUUID := uuid.Must(uuid.NewV4())
	secondUUID := uuid.Must(uuid.NewV4())
	for _, profile := range []*pb.Profile{
		{Uuid: firstUUID.String(), Id: "first-user", Name: "First", Type: "user"},
		{Uuid: secondUUID.String(), Id: "second-user", Name: "Second", Type: "user"},
	} {
		assert.Nil(s.T(), model.UpdateProfile(s.srv.mdb, profile))
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, profileUUID := range []uuid.UUID{firstUUID, secondUUID} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.srv.PostFeedinfo(ctx, &pb.Feedinfo{
				Uuid: profileUUID.String(),
				Id:   "shared-name",
				Name: "Updated",
				Type: "user",
			})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	assert.Equal(s.T(), 1, successes)
	assert.Equal(s.T(), 1, failures)

	winner, err := model.GetProfileFromUserId(s.srv.mdb, "shared-name")
	assert.Nil(s.T(), err)
	assert.Contains(s.T(), []string{firstUUID.String(), secondUUID.String()}, winner.Uuid)

	loserID := "first-user"
	if winner.Uuid == firstUUID.String() {
		loserID = "second-user"
	}
	loser, err := model.GetProfileFromUserId(s.srv.mdb, loserID)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), loserID, loser.Id)
}

// TestFetchEntryLegacyWithoutProfileUuid covers historical entries that
// predate the ProfileUuid field: FetchEntry must resolve the author via
// the From.Id fallback instead of 404ing the permalink page.
func (s *RpcTestSuite) TestFetchEntryLegacyWithoutProfileUuid() {
	profileUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{
		Uuid:    profileUUID.String(),
		Id:      "legacy",
		Name:    "Legacy User",
		Type:    "user",
		Picture: "http://example.com/legacy.jpg",
	}
	assert.Nil(s.T(), model.UpdateProfile(s.srv.mdb, profile))

	// Write the entry record directly: PutEntry rejects an empty
	// ProfileUuid today, but historical rows may lack it.
	entry := &pb.Entry{
		Id:   uuid.Must(uuid.NewV4()).String(),
		Date: "2012-09-07T07:40:22Z",
		Body: "legacy entry",
		From: &pb.Feed{Id: "legacy"},
		// no ProfileUuid
	}
	entryUUID := uuid.Must(uuid.FromString(entry.Id))
	_, err := model.Entry.Put(s.srv.rdb, entryUUID.Bytes(), entry)
	assert.Nil(s.T(), err)

	feed, err := s.srv.FetchEntry(context.Background(), &pb.EntryRequest{Uuid: entry.Id})
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "legacy", feed.Id)
	assert.Equal(s.T(), "Legacy User", feed.Name)
	got := feed.Entries[0]
	assert.Equal(s.T(), "legacy", got.From.Id)
	assert.Equal(s.T(), "Legacy User", got.From.Name)
	assert.Equal(s.T(), "http://example.com/legacy.jpg", got.From.Picture)
}

// TestCommentPrincipalRequirement verifies that comment commands carry a
// valid user_uuid; the server resolves the canonical profile from it and
// rejects missing, malformed, zero, or unknown principals instead of trusting
// client actor references.
func (s *RpcTestSuite) TestCommentPrincipalRequirement() {
	ctx := context.Background()

	profileUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{Uuid: profileUUID.String(), Id: "commenter", Name: "Commenter", Type: "user"}
	assert.Nil(s.T(), model.UpdateProfile(s.srv.mdb, profile))

	entry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: profileUUID.String(),
		Date:        time.Now().UTC().Format(time.RFC3339),
	}
	_, err := model.PutEntry(s.srv.rdb, entry)
	assert.Nil(s.T(), err)

	newComment := func() *pb.Comment {
		return &pb.Comment{
			Id:   uuid.Must(uuid.NewV4()).String(),
			Date: time.Now().UTC().Format(time.RFC3339),
			Body: "hi",
			From: &pb.Feed{Id: "commenter", Name: "Commenter"},
		}
	}

	for name, tc := range map[string]struct {
		userUuid string
		code     codes.Code
	}{
		"missing":   {"", codes.InvalidArgument},
		"malformed": {"not-a-uuid", codes.InvalidArgument},
		"zero":      {uuid.Nil.String(), codes.InvalidArgument},
		"unknown":   {uuid.Must(uuid.NewV4()).String(), codes.NotFound},
	} {
		_, err := s.srv.CommentEntry(ctx, &pb.CommentRequest{
			Entry: entry.Id, Comment: newComment(), UserUuid: tc.userUuid,
		})
		assert.Equal(s.T(), tc.code, status.Code(err), "CommentEntry %s", name)

		_, err = s.srv.DeleteComment(ctx, &pb.CommentDeleteRequest{
			Entry: entry.Id, Comment: "x", UserUuid: tc.userUuid,
		})
		assert.Equal(s.T(), tc.code, status.Code(err), "DeleteComment %s", name)
	}

	// A valid principal resolves the canonical profile.
	cReq := &pb.CommentRequest{Entry: entry.Id, Comment: newComment(), UserUuid: profile.Uuid}
	got, err := s.srv.CommentEntry(ctx, cReq)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 1, len(got.Comments))

	dReq := &pb.CommentDeleteRequest{Entry: entry.Id, Comment: got.Comments[0].Id, UserUuid: profile.Uuid}
	got, err = s.srv.DeleteComment(ctx, dReq)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 0, len(got.Comments))
}

// TestPrincipalFromUserUuid proves the canonical profile is actually
// resolved (Uuid/Id/Name), and that a deleted profile maps to NotFound.
func (s *RpcTestSuite) TestPrincipalFromUserUuid() {
	profileUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{Uuid: profileUUID.String(), Id: "principal", Name: "Principal User", Type: "user"}
	assert.Nil(s.T(), model.UpdateProfile(s.srv.mdb, profile))

	got, err := s.srv.principalFromUserUuid(profile.Uuid)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), profile.Uuid, got.Uuid)
	assert.Equal(s.T(), "principal", got.Id)
	assert.Equal(s.T(), "Principal User", got.Name)

	deletedUUID := uuid.Must(uuid.NewV4())
	assert.Nil(s.T(), model.UpdateProfile(s.srv.mdb, &pb.Profile{
		Uuid: deletedUUID.String(), Id: "ghost", Type: "user", Deleted: true,
	}))
	_, err = s.srv.principalFromUserUuid(deletedUUID.String())
	assert.Equal(s.T(), codes.NotFound, status.Code(err))
}

// TestCommentEntryOverridesForgedFrom proves the server persists the canonical
// principal as the comment author even when the caller forges comment.From as
// someone else.
func (s *RpcTestSuite) TestCommentEntryOverridesForgedFrom() {
	ctx := context.Background()

	profileUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{Uuid: profileUUID.String(), Id: "realuser", Name: "Real User", Type: "user"}
	assert.Nil(s.T(), model.UpdateProfile(s.srv.mdb, profile))
	victimUUID := uuid.Must(uuid.NewV4())
	assert.Nil(s.T(), model.UpdateProfile(s.srv.mdb, &pb.Profile{
		Uuid: victimUUID.String(), Id: "victim", Name: "Victim", Type: "user",
	}))

	entry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: profileUUID.String(),
		Date:        time.Now().UTC().Format(time.RFC3339),
	}
	_, err := model.PutEntry(s.srv.rdb, entry)
	assert.Nil(s.T(), err)

	forged := &pb.Comment{
		Id:   uuid.Must(uuid.NewV4()).String(),
		Date: time.Now().UTC().Format(time.RFC3339),
		Body: "forged identity",
		From: &pb.Feed{Uuid: victimUUID.String(), Id: "victim", Name: "Victim"},
	}
	got, err := s.srv.CommentEntry(ctx, &pb.CommentRequest{
		Entry: entry.Id, Comment: forged, UserUuid: profile.Uuid,
	})
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 1, len(got.Comments))
	from := got.Comments[0].From
	assert.Equal(s.T(), profile.Uuid, from.Uuid)
	assert.Equal(s.T(), "realuser", from.Id)
	assert.Equal(s.T(), "Real User", from.Name)
}

func (s *RpcTestSuite) TestFeedIndexLoadDump() {
	// Given FeedIndex, load and dump to db
	entryID := "c6f8dca854f011ddb489003048343a40"
	index := NewFeedIndex(nil, "public", uuid.Must(uuid.NewV4()))
	err := index.load(s.srv.mdb)
	assert.Nil(s.T(), err)

	for range 10 {
		// index.itemCh <- uuid
		index.Push(entryID)
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
	entryUUID := uuid.NewV5(uuid.NamespaceURL, name)
	entry.Id = entryUUID.String()

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
	conn, err := grpc.Dial(s.rpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
	kp.KLine = proto.Clone(kp.KLine).(*pb.KLine)
	kp.KLine.Date = int32(dt.Add(-24 * time.Hour).Unix())
	kp.KLine.High = 7788.0
	err = stream.Send(kp)
	assert.Nil(s.T(), err)

	resp, err := stream.CloseAndRecv()
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), int32(2), resp.Count)

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
	assert.Equal(s.T(), 2, len(klineResp.KLines))
	kline := klineResp.KLines[0]
	assert.EqualValues(s.T(), 8848.0, kline.High)

	// Requests above the supported maximum are capped at 3650, not reset
	// to one result.
	req.Bars = 3651
	klineResp, err = api.GetKLines(ctx, req)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 2, len(klineResp.KLines))

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

// Shutdown must stop the background job tickers and wait for them
// before closing the database, otherwise a running job panics with
// "pebble: closed" on an already closed db.
func TestShutdownStopsBackgroundJobs(t *testing.T) {
	dbpath := t.TempDir()
	cfg, err := util.NewConfigFromJSON("../conf/example.config.json")
	assert.Nil(t, err)

	search.InitMockIndexService(filepath.Join(dbpath, "index"))
	srv, err := NewApiServer(dbpath, cfg)
	require.NoError(t, err)
	srv.StartBackgroundJobs()

	done := make(chan struct{})
	go func() {
		srv.Shutdown()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown did not return: background jobs were not stopped")
	}
}

func TestShutdownIsConcurrentAndIdempotent(t *testing.T) {
	dbpath := t.TempDir()
	cfg, err := util.NewConfigFromJSON("../conf/example.config.json")
	assert.NoError(t, err)

	search.InitMockIndexService(filepath.Join(dbpath, "index"))
	srv, err := NewApiServer(dbpath, cfg)
	require.NoError(t, err)
	srv.StartBackgroundJobs()
	srv.StartBackgroundJobs()

	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			srv.Shutdown()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent Shutdown calls did not return")
	}
}

// Twitter 登录完整生命周期：首次登录创建 profile 和 twitter service；
// 重复登录保留旧 uuid（旧 uuid 可能来自 FriendFeed 迁移，绝不能重新生成）、
// 刷新 token，且不重建 profile。
func (s *RpcTestSuite) TestPutOAuthTwitterLoginLifecycle() {
	ctx := context.Background()

	// 与 httpd AuthCallback 的映射一致：Name 存 screen_name，NickName 存显示名
	first := &pb.OAuthUser{
		Name:        "screenname",
		NickName:    "Screen Name",
		UserId:      "tw-lifecycle",
		AccessToken: "token-1",
		Provider:    "twitter",
	}
	profile, err := s.srv.PutOAuth(ctx, first)
	assert.Nil(s.T(), err)
	assert.NotEmpty(s.T(), profile.Uuid)
	assert.Equal(s.T(), "screenname", profile.Id)
	assert.Equal(s.T(), "Screen Name", profile.Name)

	// twitter provider 必须创建 service
	profileUUID, err := uuid.FromString(profile.Uuid)
	assert.Nil(s.T(), err)
	services, err := model.GetServicesForProfile(s.srv.rdb, profileUUID)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 1, len(services))
	svc := services[0]
	assert.Equal(s.T(), "twitter", svc.Id)
	// 现状固化：service.Profile 拼的是 authinfo.NickName（goth 的显示名），
	// 疑似 bug（显示名含空格时 URL 无效），但字段映射历史较乱，
	// 是否修正需另行确认，本测试只锁定当前行为。
	assert.Equal(s.T(), "https://twitter.com/Screen Name", svc.Profile)
	assert.Equal(s.T(), "screenname", svc.Username)
	assert.Equal(s.T(), "token-1", svc.Oauth.AccessToken)

	// 重复登录：新 token、空 uuid
	relogin := &pb.OAuthUser{
		Name:        "screenname",
		NickName:    "Screen Name",
		UserId:      "tw-lifecycle",
		AccessToken: "token-2",
		Provider:    "twitter",
	}
	profile2, err := s.srv.PutOAuth(ctx, relogin)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), profile.Uuid, profile2.Uuid)

	// token 已刷新，uuid 未变
	_, oauthUser, err := model.GetOAuthUser(s.srv.mdb, "twitter", "tw-lifecycle")
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), profile.Uuid, oauthUser.Uuid)
	assert.Equal(s.T(), "token-2", oauthUser.AccessToken)

	// 仍然只有一个 service（重建而非追加）
	services, err = model.GetServicesForProfile(s.srv.rdb, profileUUID)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 1, len(services))
}

// Google 登录创建 profile，但不创建任何 service。
func (s *RpcTestSuite) TestPutOAuthGoogleLoginCreatesNoService() {
	ctx := context.Background()

	req := &pb.OAuthUser{
		Name:     "guser",
		NickName: "Google User",
		UserId:   "google-nosvc",
		Provider: "google",
	}
	profile, err := s.srv.PutOAuth(ctx, req)
	assert.Nil(s.T(), err)
	assert.NotEmpty(s.T(), profile.Uuid)

	profileUUID, err := uuid.FromString(profile.Uuid)
	assert.Nil(s.T(), err)
	services, err := model.GetServicesForProfile(s.srv.rdb, profileUUID)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), 0, len(services))

	// 重复登录同样保留 uuid
	profile2, err := s.srv.PutOAuth(ctx, &pb.OAuthUser{
		Name:     "guser",
		NickName: "Google User",
		UserId:   "google-nosvc",
		Provider: "google",
	})
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), profile.Uuid, profile2.Uuid)
}

// fakeMirrorStorage simulates media mirroring without network or disk IO:
// FromUrl rewrites the object URL to the mirror front domain, or fails
// outright when fail is set. It records every src it was asked to mirror.
type fakeMirrorStorage struct {
	fail bool
	srcs []string
}

func (f *fakeMirrorStorage) Exists(name string) (bool, error) { return false, nil }

func (f *fakeMirrorStorage) Fetch(obj *media.Object) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMirrorStorage) Post(obj *media.Object) (*media.Object, error) { return obj, nil }

func (f *fakeMirrorStorage) Thumbnail(obj *media.Object) (*media.Object, error) { return obj, nil }

func (f *fakeMirrorStorage) Mirror(obj *media.Object) (*media.Object, error) {
	obj.Path = obj.Filename
	obj.Url = "https://m.friendfeed.me/" + obj.Filename
	return obj, nil
}

func (f *fakeMirrorStorage) FromUrl(filename, src, mimetype string) (*media.Object, error) {
	f.srcs = append(f.srcs, src)
	if f.fail {
		return nil, errors.New("fake mirror failure")
	}
	parsed, err := url.Parse(src)
	if err != nil {
		return nil, err
	}
	if filename == "" {
		filename = strings.TrimLeft(parsed.Path, "/")
	}
	return f.Mirror(&media.Object{Filename: filename, Url: src, MimeType: mimetype})
}

func (s *RpcTestSuite) archiveOneEntry(entry *pb.Entry) *pb.FeedSummary {
	conn, err := grpc.Dial(s.rpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)
	defer conn.Close()

	stream, err := pb.NewApiClient(conn).ArchiveFeed(context.Background())
	s.Require().NoError(err)
	s.Require().NoError(stream.Send(entry))
	summary, err := stream.CloseAndRecv()
	s.Require().NoError(err)
	return summary
}

// ArchiveFeed mirrors media synchronously before PutEntry, so the persisted
// entry carries the rewritten mirrored URLs.
func (s *RpcTestSuite) TestArchiveFeedMirrorsMediaBeforePutEntry() {
	s.srv.fs = &fakeMirrorStorage{}

	entry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		Date:        "2021-07-25T15:00:00Z",
		Body:        "entry with media",
		ProfileUuid: uuid.Must(uuid.NewV4()).String(),
		Thumbnails: []*pb.Thumbnail{{
			Url:  "http://origin.example/t/thumb1.jpg",
			Link: "http://origin.example/pages/1",
		}},
		Files: []*pb.File{{
			Name: "doc.pdf",
			Url:  "http://origin.example/f/doc.pdf",
			Type: "application/pdf",
		}},
	}

	summary := s.archiveOneEntry(entry)
	assert.Equal(s.T(), int32(1), summary.EntryCount)

	persisted, err := model.GetEntry(s.srv.rdb, entry.Id)
	s.Require().NoError(err)
	assert.Equal(s.T(), "https://m.friendfeed.me/t/thumb1.jpg", persisted.Thumbnails[0].Url)
	assert.Equal(s.T(), "https://m.friendfeed.me/doc.pdf", persisted.Files[0].Url)
	assert.Equal(s.T(), "application/pdf", persisted.Files[0].Type)
}

// When mirroring fails, archiving still succeeds and the entry keeps the
// original media URLs.
func (s *RpcTestSuite) TestArchiveFeedKeepsOriginalURLWhenMirrorFails() {
	s.srv.fs = &fakeMirrorStorage{fail: true}

	entry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		Date:        "2021-07-25T15:00:00Z",
		Body:        "entry with broken media",
		ProfileUuid: uuid.Must(uuid.NewV4()).String(),
		Thumbnails: []*pb.Thumbnail{{
			Url:  "http://origin.example/t/thumb1.jpg",
			Link: "http://origin.example/pages/1",
		}},
		Files: []*pb.File{{
			Name: "doc.pdf",
			Url:  "http://origin.example/f/doc.pdf",
			Type: "application/pdf",
		}},
	}

	summary := s.archiveOneEntry(entry)
	assert.Equal(s.T(), int32(1), summary.EntryCount)

	persisted, err := model.GetEntry(s.srv.rdb, entry.Id)
	s.Require().NoError(err)
	assert.Equal(s.T(), "http://origin.example/t/thumb1.jpg", persisted.Thumbnails[0].Url)
	assert.Equal(s.T(), "http://origin.example/f/doc.pdf", persisted.Files[0].Url)
}

// thumb.Link is the click-through navigation URL, not a media resource:
// mirrorMedia must never request it, and the field is kept as is. The
// recording fake proves no fetch of the Link URL can happen, since every
// fetch goes through FromUrl.
func TestMirrorMediaNeverFetchesThumbnailLink(t *testing.T) {
	fake := &fakeMirrorStorage{}
	entry := &pb.Entry{
		Thumbnails: []*pb.Thumbnail{{
			Url:  "http://origin.example/t/thumb1.jpg",
			Link: "http://origin.example/pages/1",
		}},
	}

	srv := &ApiServer{}
	assert.NoError(t, srv.mirrorMedia(fake, entry))
	assert.Equal(t, "https://m.friendfeed.me/t/thumb1.jpg", entry.Thumbnails[0].Url)
	assert.Equal(t, "http://origin.example/pages/1", entry.Thumbnails[0].Link)
	assert.Equal(t, []string{"http://origin.example/t/thumb1.jpg"}, fake.srcs)
}

// Media beyond the per-entry object cap keep their original URLs.
func TestMirrorMediaObjectCapKeepsOriginalURLs(t *testing.T) {
	old := mirrorMediaMaxObjects
	mirrorMediaMaxObjects = 1
	defer func() { mirrorMediaMaxObjects = old }()

	fake := &fakeMirrorStorage{}
	entry := &pb.Entry{
		Files: []*pb.File{
			{Name: "a.jpg", Url: "http://origin.example/f/a.jpg"},
			{Name: "b.jpg", Url: "http://origin.example/f/b.jpg"},
		},
	}

	srv := &ApiServer{}
	assert.NoError(t, srv.mirrorMedia(fake, entry))
	assert.Equal(t, "https://m.friendfeed.me/a.jpg", entry.Files[0].Url)
	assert.Equal(t, "http://origin.example/f/b.jpg", entry.Files[1].Url)
	assert.Equal(t, []string{"http://origin.example/f/a.jpg"}, fake.srcs)
}

// Empty media records are not requests and must not consume the object cap;
// otherwise malformed legacy data can prevent a later valid object from being
// mirrored.
func TestMirrorMediaSkipsEmptyURLsWithoutUsingObjectCap(t *testing.T) {
	old := mirrorMediaMaxObjects
	mirrorMediaMaxObjects = 1
	defer func() { mirrorMediaMaxObjects = old }()

	fake := &fakeMirrorStorage{}
	entry := &pb.Entry{
		Thumbnails: []*pb.Thumbnail{nil, {Url: ""}},
		Files: []*pb.File{
			nil,
			{Name: "valid.jpg", Url: "http://origin.example/f/valid.jpg"},
		},
	}

	srv := &ApiServer{}
	assert.NoError(t, srv.mirrorMedia(fake, entry))
	assert.Equal(t, "https://m.friendfeed.me/valid.jpg", entry.Files[1].Url)
	assert.Equal(t, []string{"http://origin.example/f/valid.jpg"}, fake.srcs)
}

// Once the per-entry time budget is exhausted, remaining media keep their
// original URLs.
func TestMirrorMediaBudgetExhaustedKeepsOriginalURLs(t *testing.T) {
	old := mirrorMediaBudget
	mirrorMediaBudget = -time.Second // already exhausted
	defer func() { mirrorMediaBudget = old }()

	fake := &fakeMirrorStorage{}
	entry := &pb.Entry{
		Thumbnails: []*pb.Thumbnail{{Url: "http://origin.example/t/thumb1.jpg"}},
		Files:      []*pb.File{{Name: "a.jpg", Url: "http://origin.example/f/a.jpg"}},
	}

	srv := &ApiServer{}
	assert.NoError(t, srv.mirrorMedia(fake, entry))
	assert.Equal(t, "http://origin.example/t/thumb1.jpg", entry.Thumbnails[0].Url)
	assert.Equal(t, "http://origin.example/f/a.jpg", entry.Files[0].Url)
	assert.Empty(t, fake.srcs)
}
