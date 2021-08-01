package server

import (
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/golang/protobuf/proto"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"golang.org/x/net/context"
)

func (s *ApiServer) RefetchJobTicker() {
	t := time.Tick(2 * time.Minute)
	for range t {
		log.Printf("refetch user feeds.")
		s.RefetchUserFeed()
	}
}

func (s *ApiServer) IndexJobTicker() {
	t := time.Tick(5 * time.Minute)
	for range t {
		log.Printf("dump index to db.")
		for _, idx := range s.cached {
			idx.dump(s.mdb)
		}
	}
}

func (s *ApiServer) RefetchUserFeed() error {
	prefix := model.TableProfile
	j := 0
	n, err := s.mdb.ForwardScan(prefix.Bytes(), func(i int, k, v []byte) error {
		profile := &pb.Profile{}
		if err := proto.Unmarshal(v, profile); err != nil {
			return err
		}
		// log.Printf("RefetchUserFeed proifile: <%s, %s>", profile.Uuid, profile.Id)

		feedinfo, _ := model.GetFeedinfo(s.rdb, profile.Uuid)
		// only sync twitter service
		graph := BuildGraph(feedinfo)
		if _, ok := graph.Services["twitter"]; !ok {
			return nil
		}

		service := graph.Services["twitter"]
		if service.Oauth == nil {
			return nil
		}
		job := &pb.FeedJob{
			Uuid:    feedinfo.Uuid,
			Id:      feedinfo.Id,
			Profile: profile,
			Service: service,
			Start:   0,
			Created: time.Now().Unix(),
			Updated: time.Now().Unix(),
		}

		_, err := s.EnqueJob(context.Background(), job)
		j++
		return err
	})
	if err != nil {
		log.Println("Error on scanning user profiles:", err)
	}
	log.Printf("Jobs pulled: %d scanned, %d user feeds scheduled.", n, j)
	return err
}

func (s *ApiServer) EnqueJob(ctx context.Context, job *pb.FeedJob) (*pb.FeedJob, error) {
	// Time ordered job queue
	key := store.NewFlakeKey(model.TableJobFeed, s.mdb.NextId())

	job.Key = key.String()
	job.Created = time.Now().Unix()
	job.Updated = time.Now().Unix()

	bytes, err := proto.Marshal(job)
	if err != nil {
		return nil, err
	}
	s.mdb.Put(key.Bytes(), bytes)
	return job, nil
}

func (s *ApiServer) GetFeedJob(ctx context.Context, in *pb.Worker) (*pb.FeedJob, error) {
	s.Lock()
	defer s.Unlock()

	job, err := s.dequeJob()
	if err != nil {
		return nil, err
	}

	// Time ordered running job
	key := store.NewFlakeKey(model.TableJobRunning, s.mdb.NextId())

	job.Key = key.String()
	job.Worker = in.Id
	job.Created = time.Now().Unix()
	job.Updated = time.Now().Unix()

	bytes, err := proto.Marshal(job)
	if err != nil {
		return nil, err
	}
	s.mdb.Put(key.Bytes(), bytes)
	return job, nil
}

func (s *ApiServer) dequeJob() (*pb.FeedJob, error) {
	var job *pb.FeedJob

	key := store.NewFlakeKey(model.TableJobFeed, s.mdb.NextId())
	s.mdb.ForwardScan(key.Prefix().Bytes(), func(i int, k, v []byte) error {
		job = &pb.FeedJob{}
		if err := proto.Unmarshal(v, job); err != nil {
			return err
		}
		return &store.Error{Msg: "ok", Code: store.StopIteration}
	})

	if job == nil {
		return nil, fmt.Errorf("No more job available")
	}

	kb, _ := hex.DecodeString(job.Key)
	if err := s.mdb.Delete(kb); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *ApiServer) FinishJob(ctx context.Context, job *pb.FeedJob) (*pb.FeedJob, error) {
	kb, _ := hex.DecodeString(job.Key)
	if err := s.mdb.Delete(kb); err != nil {
		return nil, err
	}

	// indicating the feed of the target id is archived
	key := store.NewMetaKey(model.TableJobHistory, job.TargetId)
	job.Key = key.String()
	job.Status = "done"
	job.Updated = time.Now().Unix()

	bytes, err := proto.Marshal(job)
	if err != nil {
		return nil, err
	}
	s.mdb.Put(key.Bytes(), bytes)
	return job, nil
}

func (s *ApiServer) ListJobQueue(prefix store.IKey) (jobs []*pb.FeedJob, err error) {
	log.Println("listing running job...")
	s.mdb.ForwardScan(prefix.Bytes(), func(i int, key, value []byte) error {
		job := &pb.FeedJob{}
		if err := proto.Unmarshal(value, job); err != nil {
			return err
		}
		// if err = s.mdb.Delete(key); err != nil {
		// 	return err
		// }
		jobs = append(jobs, job)
		log.Println("found entry:", hex.EncodeToString(key))
		return nil
	})
	return
}

func (s *ApiServer) Command(ctx context.Context, cmd *pb.CommandRequest) (*pb.CommandResponse, error) {
	switch cmd.Command {
	case "ReportJobs":
		s.DebugJobs()
	case "ReportRunningJobs":
		s.DebugRunningJobs()
	case "PurgeJobs":
		s.PurgeJobs()
	case "FixTooMuchJobs":
		s.FixTooMuchJobs()
	case "RedoFailedJob":
		s.RedoFailedJob()
	case "RefetchUserFeed":
		s.RefetchUserFeed()
	case "TestJob":
		s.TestJob()
	// case "FixKLine":
	// s.FixKLine()
	case "MarkDelete":
		s.MarkDelete(cmd.Arg1)
	case "SuperAdmin":
		s.SuperAdmin(cmd.Arg1)
	case "DBMetrics":
		s.DBMetrics()
	}

	// TODO: nothing here
	return new(pb.CommandResponse), nil
}

func (s *ApiServer) DebugJobs() {
	jobs, err := s.ListJobQueue(model.TableJobFeed)
	if err != nil {
		log.Println("err: ", err)
	}
	for _, job := range jobs {
		log.Printf("New job: %s", job)
	}
}

func (s *ApiServer) DebugRunningJobs() {
	jobs, err := s.ListJobQueue(model.TableJobRunning)
	if err != nil {
		log.Println("err: ", err)
	}
	for _, job := range jobs {
		log.Printf("Previoud running job: %s", job)
	}
}

func (s *ApiServer) PurgeJobs() error {
	log.Println("purging all jobs...")

	prefix := model.TableJobFeed
	_, err := s.mdb.ForwardScan(prefix.Bytes(), func(i int, key, value []byte) error {
		return s.mdb.Delete(key)
	})
	if err != nil {
		return err
	}

	prefix = model.TableJobRunning
	_, err = s.mdb.ForwardScan(prefix.Bytes(), func(i int, key, value []byte) error {
		return s.mdb.Delete(key)
	})

	if err != nil {
		return err
	}
	return nil
}

func (s *ApiServer) FixTooMuchJobs() error {
	log.Println("too much jobs: purging peridoc jobs...")

	prefix := model.TableJobFeed
	_, err := s.mdb.ForwardScan(prefix.Bytes(), func(i int, k, v []byte) error {
		job := &pb.FeedJob{}
		if err := proto.Unmarshal(v, job); err != nil {
			return err
		}
		if int(job.MaxLimit) == 99 {
			return s.mdb.Delete(k)
		}
		return nil
	})
	if err != nil {
		return err
	}

	prefix = model.TableJobRunning
	_, err = s.mdb.ForwardScan(prefix.Bytes(), func(i int, k, v []byte) error {
		job := &pb.FeedJob{}
		if err := proto.Unmarshal(v, job); err != nil {
			return err
		}
		if int(job.MaxLimit) == 99 {
			return s.mdb.Delete(k)
		}
		return nil
	})

	if err != nil {
		return err
	}
	return nil
}

func (s *ApiServer) RedoFailedJob() error {
	log.Println("redo failed jobs...")

	prefix := model.TableJobRunning
	_, err := s.mdb.ForwardScan(prefix.Bytes(), func(i int, k, v []byte) error {
		job := &pb.FeedJob{}
		if err := proto.Unmarshal(v, job); err != nil {
			return err
		}

		_, err := s.EnqueJob(context.Background(), job)
		if err != nil {
			return s.mdb.Delete(k)
		}
		return nil
	})

	if err != nil {
		return err
	}
	return nil
}

func (s *ApiServer) TestJob() error {
	profile, _ := model.GetProfileFromUserId(s.mdb, "yinhm")
	feedinfo, _ := model.GetFeedinfo(s.rdb, profile.Uuid)
	// only sync twitter service
	graph := BuildGraph(feedinfo)
	if _, ok := graph.Services["twitter"]; !ok {
		return nil
	}

	service := graph.Services["twitter"]
	if service.Oauth == nil {
		return nil
	}
	job := &pb.FeedJob{
		Uuid:    feedinfo.Uuid,
		Id:      feedinfo.Id,
		Profile: profile,
		Service: service,
		Start:   0,
		Created: time.Now().Unix(),
		Updated: time.Now().Unix(),
	}

	_, err := s.EnqueJob(context.Background(), job)
	return err
}

func (s *ApiServer) MarkDelete(feedId string) (bool, error) {
	profile, err := model.GetProfileFromUserId(s.mdb, feedId)
	if err != nil {
		return false, err
	}
	profile.Deleted = true
	model.UpdateProfile(s.mdb, profile)
	return true, nil
}

// Mark user as superadmin
// re-run will remove user from superadmin
func (s *ApiServer) SuperAdmin(id string) (bool, error) {
	profile, err := model.GetProfileFromUserId(s.mdb, id)
	if err != nil {
		return false, err
	}
	profile.IsSuper = !profile.IsSuper
	model.UpdateProfile(s.mdb, profile)
	logger.Warnf("SuperAdmin toggle: <%s, is_super=%t>", profile.Id, profile.IsSuper)
	return true, nil
}

func (s *ApiServer) FixKLine() error {
	db := s.rdb
	batch := db.NewBatch()
	prefix := model.KLine.Prefix
	prefix = append(prefix, prefix...)
	logger.Debugf("prefix range delete: %s", hex.EncodeToString(prefix))
	err := batch.DeleteRange(prefix, store.KeyUpperBound(prefix), pebble.NoSync)
	if err != nil {
		return err
	}
	if err = batch.Commit(pebble.Sync); err != nil {
		return err
	}
	if err = batch.Close(); err != nil {
		return err
	}
	return nil
}

func (s *ApiServer) DBMetrics() error {
	db := s.rdb
	metrics := db.Metrics()
	logger.Printf("\n%s\n", metrics.String())
	return nil
}
