package server

import (
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/golang/protobuf/proto"
	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
	"golang.org/x/net/context"
)

func (s *ApiServer) RefetchJobTicker() {
	t := time.Tick(15 * time.Minute)
	for _ = range t {
		log.Printf("refetch user feeds.")
		s.RefetchUserFeed()
	}
}

func (s *ApiServer) IndexJobTicker() {
	t := time.Tick(5 * time.Minute)
	for _ = range t {
		log.Printf("dump index to db.")
		for _, idx := range s.cached {
			idx.dump(s.mdb)
		}
	}
}

func (s *ApiServer) RefetchUserFeed() error {
	prefix := store.TableProfile
	j := 0
	n, err := store.ForwardTableScan(s.mdb, prefix, func(i int, k, v []byte) error {
		profile := &pb.Profile{}
		if err := proto.Unmarshal(v, profile); err != nil {
			return err
		}

		if profile.RemoteKey == "" {
			return nil
		}

		job := &pb.FeedJob{
			Uuid:        profile.Uuid,
			Id:          profile.Id,
			RemoteKey:   profile.RemoteKey,
			TargetId:    profile.Id,
			Start:       0,
			PageSize:    100,
			MaxLimit:    99,
			ForceUpdate: true,
			Created:     time.Now().Unix(),
			Updated:     time.Now().Unix(),
		}

		log.Println(job)
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

func (s *ApiServer) RefetchFriendFeed() error {
	prefix := store.TableProfile
	j := 0
	n, err := store.ForwardTableScan(s.mdb, prefix, func(i int, k, v []byte) error {
		profile := &pb.Profile{}
		if err := proto.Unmarshal(v, profile); err != nil {
			return err
		}

		if profile.RemoteKey != "" {
			return nil
		}

		oldjob, err := store.GetArchiveHistory(s.mdb, profile.Id)
		if err != nil {
			return err
		}

		if oldjob.Id == "" || oldjob.RemoteKey == "" {
			return fmt.Errorf("Refetch Friendfeed: unknown remote key")
		}

		job := &pb.FeedJob{
			Uuid:        profile.Uuid,
			Id:          profile.Id,
			RemoteKey:   profile.RemoteKey,
			TargetId:    profile.Id,
			Start:       0,
			PageSize:    100,
			MaxLimit:    99,
			ForceUpdate: true,
			Created:     time.Now().Unix(),
			Updated:     time.Now().Unix(),
		}

		log.Println(job)
		_, err = s.EnqueJob(context.Background(), job)
		j++
		return err
	})
	if err != nil {
		log.Println("Error on scanning user profiles:", err)
	}
	log.Printf("Jobs pulled: %d scanned, %d friendfeed feeds scheduled.", n, j)
	return err
}

func (s *ApiServer) EnqueJob(ctx context.Context, job *pb.FeedJob) (*pb.FeedJob, error) {
	// Time ordered job queue
	key := store.NewFlakeKey(store.TableJobFeed, s.mdb.NextId())
	log.Printf("enque job: %s", key.String())

	if job.TargetId == "" {
		job.TargetId = job.Id
	}

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
		log.Println("deque err:", err)
		return nil, err
	}

	// Time ordered running job
	key := store.NewFlakeKey(store.TableJobRunning, s.mdb.NextId())

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
	// derived job
	var firstJob *pb.FeedJob
	// high priority job
	var pqJob *pb.FeedJob

	key := store.NewFlakeKey(store.TableJobFeed, s.mdb.NextId())

	store.ForwardTableScan(s.mdb, key.Prefix(), func(i int, k, v []byte) error {
		job := &pb.FeedJob{}
		if err := proto.Unmarshal(v, job); err != nil {
			return err
		}
		if firstJob == nil {
			firstJob = job
		}
		if job.Id == job.TargetId {
			pqJob = job
			return &store.Error{"ok", store.StopIteration}
		}

		if i > 1000 {
			return &store.Error{"ok", store.StopIteration}
		}

		return nil
	}) // WARN: err maybe ok error

	if pqJob == nil && firstJob == nil {
		return nil, fmt.Errorf("No more job available")
	}

	job := pqJob
	if job == nil {
		job = firstJob
	}

	kb, _ := hex.DecodeString(job.Key)
	if err := s.mdb.Delete(kb); err != nil {
		return nil, err
	}
	log.Printf("dequeue job: %s", job.Key)
	return job, nil
}

func (s *ApiServer) FinishJob(ctx context.Context, job *pb.FeedJob) (*pb.FeedJob, error) {
	kb, _ := hex.DecodeString(job.Key)
	if err := s.mdb.Delete(kb); err != nil {
		return nil, err
	}

	// indicating the feed of the target id is archived
	key := store.NewMetaKey(store.TableJobHistory, job.TargetId)
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

func (s *ApiServer) ListJobQueue(prefix store.Key) (jobs []*pb.FeedJob, err error) {
	log.Println("listing running job...")
	store.ForwardTableScan(s.mdb, prefix, func(i int, key, value []byte) error {
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
	case "FixJobs":
		s.FixJobs()
	case "FixTooMuchJobs":
		s.FixTooMuchJobs()
	case "RedoFailedJob":
		s.RedoFailedJob()
	case "RefetchUserFeed":
		s.RefetchUserFeed()
	case "RefetchFriendFeed":
		s.RefetchFriendFeed()
	}

	// TODO: nothing here
	return new(pb.CommandResponse), nil
}

func (s *ApiServer) DebugJobs() {
	jobs, err := s.ListJobQueue(store.TableJobFeed)
	if err != nil {
		log.Println("err: ", err)
	}
	for _, job := range jobs {
		log.Printf("New job: %s", job)
	}
}

func (s *ApiServer) DebugRunningJobs() {
	jobs, err := s.ListJobQueue(store.TableJobRunning)
	if err != nil {
		log.Println("err: ", err)
	}
	for _, job := range jobs {
		log.Printf("Previoud running job: %s", job)
	}
}

func (s *ApiServer) PurgeJobs() error {
	log.Println("purging all jobs...")

	prefix := store.TableJobFeed
	_, err := store.ForwardTableScan(s.mdb, prefix, func(i int, key, value []byte) error {
		return s.mdb.Delete(key)
	})
	if err != nil {
		return err
	}

	prefix = store.TableJobRunning
	_, err = store.ForwardTableScan(s.mdb, prefix, func(i int, key, value []byte) error {
		return s.mdb.Delete(key)
	})

	if err != nil {
		return err
	}
	return nil
}

func (s *ApiServer) FixJobs() error {
	log.Println("purging all jobs...")

	prefix := store.TableJobFeed
	_, err := store.ForwardTableScan(s.mdb, prefix, func(i int, k, v []byte) error {
		job := &pb.FeedJob{}
		if err := proto.Unmarshal(v, job); err != nil {
			return err
		}
		if job.RemoteKey == "" {
			return s.mdb.Delete(k)
		}
		return nil
	})
	if err != nil {
		return err
	}

	prefix = store.TableJobRunning
	_, err = store.ForwardTableScan(s.mdb, prefix, func(i int, k, v []byte) error {
		job := &pb.FeedJob{}
		if err := proto.Unmarshal(v, job); err != nil {
			return err
		}
		if job.RemoteKey == "" {
			return s.mdb.Delete(k)
		}
		return nil
	})

	if err != nil {
		return err
	}
	return nil
}

func (s *ApiServer) FixTooMuchJobs() error {
	log.Println("too much jobs: purging peridoc jobs...")

	prefix := store.TableJobFeed
	_, err := store.ForwardTableScan(s.mdb, prefix, func(i int, k, v []byte) error {
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

	prefix = store.TableJobRunning
	_, err = store.ForwardTableScan(s.mdb, prefix, func(i int, k, v []byte) error {
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

	prefix := store.TableJobRunning
	_, err := store.ForwardTableScan(s.mdb, prefix, func(i int, k, v []byte) error {
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

func (s *ApiServer) RebuildPublicFeed() {
	prefix := store.TableProfile
	store.ForwardTableScan(s.mdb, prefix, func(i int, key, value []byte) error {
		item := &pb.Profile{}
		if err := proto.Unmarshal(value, item); err != nil {
			return err
		}
		// seek first 100 feeds?
		log.Println("profile: %s", item.Id)
		return nil
	})
	return
}
