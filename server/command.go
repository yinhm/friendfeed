package server

import (
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"context"
	"github.com/cockroachdb/pebble/v2"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func (s *ApiServer) Command(ctx context.Context, cmd *pb.CommandRequest) (*pb.CommandResponse, error) {
	switch cmd.Command {
	case "ReportJobs":
		s.DebugJobs()
	case "ReportRunningJobs":
		s.DebugRunningJobs()
	case "PurgeJobs":
		if err := s.PurgeJobs(); err != nil {
			return nil, err
		}
	case "FixTooMuchJobs":
		if err := s.FixTooMuchJobs(); err != nil {
			return nil, err
		}
	case "RedoFailedJob":
		if err := s.RedoFailedJob(); err != nil {
			return nil, err
		}
	case "RefetchUserFeed":
		if err := s.RefetchUserFeed(); err != nil {
			return nil, err
		}
	case "TestJob":
		if err := s.TestJob(); err != nil {
			return nil, err
		}
	case "PurgePrefix":
		if err := s.PurgePrefix(model.Feedinfo.Prefix); err != nil {
			return nil, err
		}
	case "MarkDelete":
		if _, err := s.MarkDelete(cmd.Arg1); err != nil {
			return nil, err
		}
	case "SuperAdmin":
		if _, err := s.SuperAdmin(cmd.Arg1); err != nil {
			return nil, err
		}
	case "BackupDB":
		if err := s.BackupDB(); err != nil {
			return nil, err
		}
	case "DBMetrics":
		if err := s.DBMetrics(); err != nil {
			return nil, err
		}
	case "CreateSystemProfile":
		if err := s.CreateSystemProfile(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown command %q", cmd.Command)
	}

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
			// Keep the running record so the job is retried next time.
			return err
		}
		return s.mdb.Delete(k)
	})

	if err != nil {
		return err
	}
	return nil
}

func (s *ApiServer) TestJob() error {
	profile, _ := model.GetProfileFromUserId(s.mdb, "yinhm")
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

func (s *ApiServer) PurgePrefix(prefix store.Key) error {
	db := s.rdb
	batch := db.NewBatch()
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

// BackupDB copies a point-in-time snapshot of the live database to
// /tmp/backup-YYYYMMDD-HHMMSS. The timestamp suffix keeps repeated backups on
// the same day from colliding; BackupDBTo still fails if the same second is
// reused. It keeps the historical production /tmp destination; tests drive
// the same logic through BackupDBTo with an explicit destination path.
func (s *ApiServer) BackupDB() error {
	backupFolder := time.Now().Format("backup-20060102-150405")
	backupPath := filepath.Join("/tmp", backupFolder)
	return s.BackupDBTo(backupPath)
}

// BackupDBTo copies a point-in-time snapshot of the live database into a new
// store at destPath: the copy is read through a Pebble snapshot taken up
// front, so concurrent writes during the copy do not leak in or tear related
// records. The destination directory must not exist — its parent is created
// if necessary and destPath itself is created atomically, so rerunning a
// backup into an existing directory fails instead of merging with (and
// resurrecting deleted keys from) an earlier copy. The destination store is
// closed before returning, so destPath can be reopened (e.g. as a restored
// database) right away.
func (s *ApiServer) BackupDBTo(destPath string) error {
	log.Println("BackupDB...")

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("create backup parent dir: %w", err)
	}
	if err := os.Mkdir(destPath, 0755); err != nil {
		return fmt.Errorf("create backup dir %s: %w", destPath, err)
	}

	// Snapshot before iterating so the backup is consistent as of this point;
	// both the iterator and the snapshot must be closed.
	snap := s.rdb.Snapshot()
	defer snap.Close()
	iter := s.rdb.SnapshotIterator(snap)
	defer iter.Close()

	logger.Warnf("db backup to: %s", destPath)

	ndb := store.NewStore(destPath)
	ndb.SetSync(false)
	defer ndb.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		if err := ndb.Set(iter.Key(), iter.Value()); err != nil {
			return fmt.Errorf("backup key %x: %w", iter.Key(), err)
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterate source database: %w", err)
	}
	return nil
}

func (s *ApiServer) CreateSystemProfile() error {
	log.Println("CreateSystemProfile...")

	// for stock backtesting
	feedinfo := &pb.Feedinfo{
		Uuid:        fmt.Sprintf("%x", model.UniqueKeyFrom("hiqt")),
		Id:          "hiqt",
		Name:        "hiqt",
		Type:        "group",
		Private:     false,
		Description: fmt.Sprintf("<%s>", "hiqt"),
	}
	_, err := s.PostFeedinfo(context.Background(), feedinfo)
	if err != nil {
		return err
	}
	logger.Debugf("Profile created: <%s, %s>", "hiqt", feedinfo.Uuid)
	return nil
}
