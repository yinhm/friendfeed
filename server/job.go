package server

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func (s *ApiServer) RefetchJobTicker() {
	if !s.beginBackgroundJob() {
		return
	}
	defer s.wg.Done()

	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			log.Printf("refetch user feeds.")
			s.RefetchUserFeed()
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
		// log.Printf("RefetchUserFeed profile: <%s, %s>", profile.Uuid, profile.Id)
		feedinfo := model.ProfileToFeedinfo(profile)
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
	if err := s.mdb.Put(key.Bytes(), bytes); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *ApiServer) GetFeedJob(ctx context.Context, in *pb.Worker) (*pb.FeedJob, error) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()

	job, queuedKey, err := s.peekQueuedJob()
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
	if err := s.mdb.ApplyBatch(func(batch *pebble.Batch) error {
		if err := batch.Delete(queuedKey, nil); err != nil {
			return err
		}
		return batch.Set(key.Bytes(), bytes, nil)
	}); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *ApiServer) peekQueuedJob() (*pb.FeedJob, store.Key, error) {
	var job *pb.FeedJob
	var queuedKey store.Key

	key := store.NewFlakeKey(model.TableJobFeed, s.mdb.NextId())
	_, err := s.mdb.ForwardScan(key.Prefix().Bytes(), func(i int, k, v []byte) error {
		job = &pb.FeedJob{}
		if err := proto.Unmarshal(v, job); err != nil {
			return err
		}
		// ForwardScan reuses iterator buffers; retain the exact database key
		// rather than trusting the denormalized job.Key field.
		queuedKey = append(store.Key(nil), k...)
		return &store.Error{Msg: "ok", Code: store.StopIteration}
	})
	if err != nil {
		return nil, nil, err
	}

	if job == nil {
		return nil, nil, errors.New("no more job available")
	}
	return job, queuedKey, nil
}

func (s *ApiServer) dequeJob() (*pb.FeedJob, error) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()

	job, queuedKey, err := s.peekQueuedJob()
	if err != nil {
		return nil, err
	}
	if err := s.mdb.Delete(queuedKey); err != nil {
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
	if err := s.mdb.Put(key.Bytes(), bytes); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *ApiServer) ListJobQueue(prefix store.IKey) (jobs []*pb.FeedJob, err error) {
	log.Println("listing running job...")
	_, err = s.mdb.ForwardScan(prefix.Bytes(), func(i int, key, value []byte) error {
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
