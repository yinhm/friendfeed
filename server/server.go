package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
	taskqueue "github.com/yinhm/friendfeed/task"
	"github.com/yinhm/friendfeed/util"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	homeTimelineMaintenanceLimit = 8
	homeTimelineFailureBackoff   = 30 * time.Second
)

// server implementation.
type ApiServer struct {
	pb.UnimplementedRealtimeServer

	// profileUpdateMu is also the coarse relationship/privacy mutation lock:
	// profile privacy flips, follow writes/requests and approvals serialize on
	// it so the privacy decision cannot change between authorization and write.
	profileUpdateMu sync.Mutex
	jobMu           sync.Mutex
	// entryLifecycleMu lets independent interaction rows mutate concurrently,
	// while keeping them mutually exclusive with Entry create/edit/delete.
	entryLifecycleMu    sync.RWMutex
	timelineMaintenance singleflight.Group
	timelineBuildSlots  chan struct{}
	timelineFailureMu   sync.Mutex
	timelineRetryAfter  map[uuid.UUID]time.Time

	// meta database
	mdb *store.Store
	// block database
	rdb *store.Store
	// file system
	fs media.Storage

	// Public timeline trim state: bumps accumulates events since the last
	// trim, trimming guarantees at most one background trim at a time.
	// Trimming never runs inside a request path.
	publicTimelineBumps    atomic.Int64
	publicTimelineTrimming atomic.Bool

	// background job lifecycle: done signals shutdown, wg tracks
	// running job goroutines so Shutdown can wait for them before
	// closing the database.
	done chan struct{}
	wg   sync.WaitGroup

	lifecycleMu           sync.Mutex
	backgroundJobsStarted bool
	shuttingDown          bool
	shutdownOnce          sync.Once
	tasks                 *taskqueue.Queue
	taskCtx               context.Context
	taskCancel            context.CancelFunc
	taskWorkersStarted    bool
	taskWorkerPollMin     time.Duration
	taskWorkerPollMax     time.Duration
	serviceFetch          serviceFetcher
	rssNow                func() time.Time
	rssHostLocks          [64]sync.Mutex
	realtime              *realtimeBus
}

// grpcSlogLogger routes gRPC internal logs into slog. gRPC fatal-level
// messages are logged as errors: library code must not terminate the process.
type grpcSlogLogger struct{}

func (grpcSlogLogger) Info(args ...any)                 { slog.Info(fmt.Sprint(args...)) }
func (grpcSlogLogger) Infoln(args ...any)               { slog.Info(fmt.Sprintln(args...)) }
func (grpcSlogLogger) Infof(format string, args ...any) { slog.Info(fmt.Sprintf(format, args...)) }

func (grpcSlogLogger) Warning(args ...any)                 { slog.Warn(fmt.Sprint(args...)) }
func (grpcSlogLogger) Warningln(args ...any)               { slog.Warn(fmt.Sprintln(args...)) }
func (grpcSlogLogger) Warningf(format string, args ...any) { slog.Warn(fmt.Sprintf(format, args...)) }

func (grpcSlogLogger) Error(args ...any)                 { slog.Error(fmt.Sprint(args...)) }
func (grpcSlogLogger) Errorln(args ...any)               { slog.Error(fmt.Sprintln(args...)) }
func (grpcSlogLogger) Errorf(format string, args ...any) { slog.Error(fmt.Sprintf(format, args...)) }

func (grpcSlogLogger) Fatal(args ...any)                 { slog.Error(fmt.Sprint(args...)) }
func (grpcSlogLogger) Fatalln(args ...any)               { slog.Error(fmt.Sprintln(args...)) }
func (grpcSlogLogger) Fatalf(format string, args ...any) { slog.Error(fmt.Sprintf(format, args...)) }

// V maps gRPC verbosity onto slog levels: level 0 always logs, higher
// verbosity requires the default logger to have debug enabled.
func (grpcSlogLogger) V(l int) bool {
	return l <= 0 || slog.Default().Enabled(context.Background(), slog.LevelDebug)
}

func init() {
	grpclog.SetLoggerV2(grpcSlogLogger{})
}

func NewApiServer(dbpath string, cfg *util.Config) (*ApiServer, error) {
	rdb, err := store.NewStore(dbpath)
	if err != nil {
		return nil, err
	}
	// The legacy meta store was unified into rdb; mdb is kept as an alias.
	mdb := rdb

	srv := &ApiServer{
		mdb:                mdb,
		rdb:                rdb,
		done:               make(chan struct{}),
		timelineBuildSlots: make(chan struct{}, homeTimelineMaintenanceLimit),
		timelineRetryAfter: make(map[uuid.UUID]time.Time),
		taskWorkerPollMin:  time.Second,
		taskWorkerPollMax:  30 * time.Second,
		rssNow:             func() time.Time { return time.Now().UTC() },
		realtime:           newRealtimeBus(),
	}
	srv.taskCtx, srv.taskCancel = context.WithCancel(context.Background())
	srv.serviceFetch = fetchServiceHTTP
	taskRegistry, err := NewTaskRegistry(srv.handleServiceTask, srv.handleHomeRebuildTask)
	if err != nil {
		rdb.Close()
		return nil, fmt.Errorf("initialize task registry: %w", err)
	}
	srv.tasks, err = taskqueue.NewQueue(rdb, taskRegistry, taskqueue.Options{})
	if err != nil {
		rdb.Close()
		return nil, fmt.Errorf("initialize task queue: %w", err)
	}

	srv.fs = media.NewStorage(cfg, 1024)
	return srv, nil
}

// StartBackgroundJobs starts periodic refetch and task queue maintenance.
// They are stopped by Shutdown before the database is closed.
func (s *ApiServer) StartBackgroundJobs() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.backgroundJobsStarted || s.shuttingDown {
		return
	}
	s.backgroundJobsStarted = true
	go s.RefetchJobTicker()
	go s.TaskReapLoop()
	go s.ServiceScheduleLoop()
	s.startTaskWorkersLocked()
}

func (s *ApiServer) beginBackgroundJob() bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.shuttingDown {
		return false
	}
	s.wg.Add(1)
	return true
}

func (s *ApiServer) Shutdown() {
	s.shutdownOnce.Do(func() {
		s.StopTaskClaims()
		s.taskCancel()
		// Prevent new background jobs from starting, then wait for existing
		// jobs to finish using the database.
		s.lifecycleMu.Lock()
		s.shuttingDown = true
		close(s.done)
		s.lifecycleMu.Unlock()
		// Shutdown may be called directly in tests or embedding programs without
		// the production BeginShutdown phase. Stop the broadcaster before
		// waiting, otherwise a permanent stream remains in wg forever.
		if s.realtime != nil {
			s.realtime.stop()
		}
		s.wg.Wait()

		s.rdb.Close()
		// s.mdb.Close()
		s.rdb = nil
		s.mdb = nil
	})
}

func (s *ApiServer) Destroy() {
	slog.Warn("Destroy db...")
	s.rdb.Destroy()
	s.mdb.Destroy()
}

// NOT USED OR TESTED?
// func (s *ApiServer) FetchFeedinfo(ctx context.Context, req *pb.ProfileRequest) (*pb.Feedinfo, error) {
// 	if req.Uuid == "" {
// 		return nil, fmt.Errorf("bad request")
// 	}
// 	return model.GetFeedinfo(s.rdb, req.Uuid)
// }

// PostFeedinfo creates or updates a profile. Updates patch the editable
// fields onto the stored profile so system-only fields (IsSuper, Deleted)
// are preserved; Feedinfo does not carry them.
func (s *ApiServer) PostFeedinfo(ctx context.Context, in *pb.Feedinfo) (*pb.Profile, error) {
	// Keep profile edits and the privacy/follow state machine in one coarse
	// mutation critical section. RenameProfileId still makes its key changes
	// atomic; this lock also prevents follow decisions from racing a privacy
	// flip and writing an edge under a stale public/private snapshot.
	s.profileUpdateMu.Lock()
	defer s.profileUpdateMu.Unlock()

	profileUUID, err := uuid.FromString(in.Uuid)
	if err != nil {
		return nil, err
	}

	// Fetch the current profile to detect ID changes
	currentProfile, err := model.GetProfileFromUuid(s.mdb, profileUUID)
	if err != nil {
		// Profile doesn't exist yet; this is a create, not an update.
		// Proceed with the standard path.
		currentProfile = nil
	}

	profile := &pb.Profile{
		Uuid:        in.Uuid,
		Id:          in.Id,
		Name:        in.Name,
		Type:        in.Type,
		Private:     in.Private,
		Picture:     in.Picture,
		Description: in.Description,
	}
	// Random wallpaper as default icon
	if profile.Picture == "" {
		profile.Picture = RandomPictureFromWallpaper(s.rdb, profile)
	}
	slog.Debug("profile pic", "id", profile.Id, "picture", profile.Picture)

	if currentProfile != nil {
		// Update: patch editable fields onto the stored profile so
		// system-only fields (IsSuper, Deleted) survive the write.
		// If the ID is changing, use RenameProfileId to handle UserMap
		// updates atomically first.
		if currentProfile.Id != profile.Id {
			if err := model.RenameProfileId(s.mdb, profileUUID, profile.Id); err != nil {
				return nil, err
			}
			// RenameProfileId updates Profile.Id but not the other fields;
			// fetch the renamed profile and apply the remaining updates.
			currentProfile, err = model.GetProfileFromUuid(s.mdb, profileUUID)
			if err != nil {
				return nil, err
			}
		}
		currentProfile.Name = profile.Name
		currentProfile.Type = profile.Type
		currentProfile.Private = profile.Private
		currentProfile.Picture = profile.Picture
		currentProfile.Description = profile.Description
		if err := model.UpdateProfile(s.mdb, currentProfile); err != nil {
			return nil, err
		}
		if !currentProfile.Private {
			// Requests only exist while the target is private. Run this on
			// every public profile update, not just the transition edge, so a
			// prior cleanup failure is retried on the next save. GraphFollow,
			// RequestFollow and ApproveFollowRequest share this lock, so no
			// new pending request can slip in between the profile write and
			// this cleanup.
			if err := s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
				return model.StageDeleteFollowRequestsByTarget(s.rdb, batch, profileUUID)
			}); err != nil {
				return nil, err
			}
		}
		profile = currentProfile
	} else {
		if err := model.UpdateProfile(s.mdb, profile); err != nil {
			return nil, err
		}
	}

	// save all feed info in one key for simplicity
	// TODO: refactor?
	// in.Entries = []*pb.Entry{}
	// if err := model.PutFeedinfo(s.rdb, profile.Uuid, in); err != nil {
	// 	return nil, err
	// }
	return profile, nil
}

// TODO: build graph if it not exists
func (s *ApiServer) FetchGraph(ctx context.Context, req *pb.ProfileRequest) (*pb.Graph, error) {
	if req.Uuid == "" {
		return nil, errors.New("bad request")
	}
	profileUuid, err := uuid.FromString(req.Uuid)
	if err != nil {
		return nil, err
	}
	profile, err := model.GetProfileFromUuid(s.rdb, profileUuid)
	if err != nil {
		return nil, err
	}
	feedinfo := model.ProfileToFeedinfo(profile)

	// scan services
	ss, _ := model.GetFeedServices(s.rdb, profileUuid)
	feedinfo.Services = ss

	return BuildGraph(feedinfo), nil
}

func (s *ApiServer) ArchiveFeed(stream pb.Api_ArchiveFeedServer) error {
	var entryCount int32
	var dateStart string
	var dateEnd string
	var lastEntry *pb.Entry
	startTime := time.Now()
	privateFeeds := make(map[string]bool)

	// tooMuchExistsItem := 0
	for {
		entry, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.FeedSummary{
				EntryCount:  entryCount,
				DateStart:   dateStart,
				DateEnd:     dateEnd,
				ElapsedTime: int32(time.Since(startTime).Seconds()),
			})
		}
		if err != nil {
			return err
		}
		entryCount++
		// Mirror media synchronously before PutEntry so the rewritten
		// mirrored URLs are persisted with the entry.
		s.mirrorMedia(s.fs, entry)
		// key, err := store.PutEntry(s.rdb, entry, false) // always use false
		created, err := s.entryCreated(entry)
		if err != nil {
			return err
		}
		s.entryLifecycleMu.Lock()
		_, err = model.PutEntry(s.rdb, entry)
		s.entryLifecycleMu.Unlock()
		if err != nil {
			log.Println("db error:", err)
			continue
		}
		// Only newly created entries bump the public timeline; re-archiving
		// an existing entry must not churn it.
		if created {
			if err := s.bumpPublicTimeline(entry, privateFeeds); err != nil {
				return err
			}
		}

		if lastEntry == nil {
			dateEnd = entry.Date
		}
		lastEntry = entry
		dateStart = lastEntry.Date
	}
}

func (s *ApiServer) ForceArchiveFeed(stream pb.Api_ForceArchiveFeedServer) error {
	var entryCount int32
	var dateStart string
	var dateEnd string
	var lastEntry *pb.Entry
	startTime := time.Now()
	privateFeeds := make(map[string]bool)

	// tooMuchExistsItem := 0
	for {
		entry, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.FeedSummary{
				EntryCount:  entryCount,
				DateStart:   dateStart,
				DateEnd:     dateEnd,
				ElapsedTime: int32(time.Since(startTime).Seconds()),
			})
		}
		if err != nil {
			return err
		}
		entryCount++
		// save db
		created, err := s.entryCreated(entry)
		if err != nil {
			return err
		}
		s.entryLifecycleMu.Lock()
		_, err = model.PutEntry(s.rdb, entry)
		s.entryLifecycleMu.Unlock()
		if err != nil {
			log.Println("db error:", err)
			continue
		}
		if created {
			if err := s.bumpPublicTimeline(entry, privateFeeds); err != nil {
				return err
			}
		}

		if lastEntry == nil {
			dateEnd = entry.Date
		}
		lastEntry = entry
		dateStart = lastEntry.Date
	}
}

// Bounds for mirroring a single entry's media: mirroring runs
// synchronously inside ArchiveFeed, so one entry must not stall archiving
// for (#media x fetch timeout). Once the time budget or the object cap is
// exhausted, the remaining media keep their original URLs. They are
// package variables so tests can shrink them.
var (
	mirrorMediaBudget     = 90 * time.Second
	mirrorMediaMaxObjects = 20
)

// mirrorMedia mirrors the entry's thumbnails and files into media storage
// and rewrites their URLs to the mirrored address. It runs before
// model.PutEntry so the mirrored URLs persist with the entry.
//
// thumb.Link is the navigation target of the thumbnail, not a media
// resource: it is kept as is and never fetched.
//
// Failure policy: when a single media object fails to mirror, the error is
// logged and the original URL is kept; archiving itself must not fail
// because of mirroring. Media beyond the per-entry time budget or object
// cap also keep their original URLs.
func (s *ApiServer) mirrorMedia(client media.Storage, entry *pb.Entry) error {
	// twitpic should be fine, see: http://blog.twitpic.com/2014/10/twitpics-future/
	deadline := time.Now().Add(mirrorMediaBudget)
	attempted := 0
	mirror := func(name, src, mimetype string) (string, bool) {
		if src == "" {
			return "", false
		}
		if attempted >= mirrorMediaMaxObjects || time.Now().After(deadline) {
			return "", false
		}
		attempted++
		newObj, err := client.FromUrl(name, src, mimetype)
		if err != nil {
			log.Println("Mirror media failed:", err)
			return "", false
		}
		return newObj.Url, true
	}

	for _, thumb := range entry.Thumbnails {
		if thumb == nil {
			continue
		}
		if mirrored, ok := mirror("", thumb.Url, ""); ok {
			thumb.Url = mirrored // rewrote to mirrored
		}
	}

	for _, file := range entry.Files {
		if file == nil {
			continue
		}
		if mirrored, ok := mirror(file.Name, file.Url, file.Type); ok {
			file.Url = mirrored // rewrote to mirrored
		}
	}
	return nil
}

// FetchFeed returns builded feed which populated data
// from user profile and entries scaned from EntryIndex.
func (s *ApiServer) FetchFeed(ctx context.Context, req *pb.FeedRequest) (*pb.Feed, error) {
	slog.Info("FetchFeed", "id", req.Id)
	if req.CursorPaging || req.ProfileUuid != "" || isPublicFeedRequest(req) {
		return s.ForwardFetchFeedWithCursor(ctx, req)
	}
	return s.ForwardFetchFeed(ctx, req)
}

func (s *ApiServer) ForwardFetchFeed(ctx context.Context, req *pb.FeedRequest) (*pb.Feed, error) {
	if req.PageSize <= 0 || req.PageSize >= 100 {
		req.PageSize = 50
	}
	slog.Debug("ForwardFetchFeed: request", "req", req)

	var profile *pb.Profile
	var err error
	var prefix []byte

	if req.ProfileUuid != "" { // user timeline
		profileUuid, _ := uuid.FromString(req.ProfileUuid)
		profile, err = model.GetProfileFromUuid(s.mdb, profileUuid)
		if err != nil {
			slog.Debug("ForwardFetchFeed: profile not found", "id", req.Id, "profile_uuid", req.ProfileUuid)
			return nil, status.Errorf(codes.NotFound, "profile not found")
		}
		fanoutUuid := model.TimelineUUID(profileUuid)
		prefix = store.NewUUIDKey(model.TableEntryIndex, fanoutUuid).Bytes()
	} else { // from user.id
		profile, err = model.GetProfileFromUserId(s.mdb, req.Id)
		if err != nil {
			profile, err = model.GetProfileFromRenameId(s.mdb, req.Id)
			if err != nil {
				slog.Debug("ForwardFetchFeed: profile not found", "id", req.Id, "err", err)
				return nil, status.Errorf(codes.NotFound, "profile not found")
			}
			slog.Debug("ForwardFetchFeed: resolved previous profile ID", "from", req.Id, "to", profile.Id)
		}
		slog.Debug("ForwardFetchFeed: profile", "profile", profile)
		profileUuid, _ := uuid.FromString(profile.Uuid)
		prefix = store.NewUUIDKey(model.TableEntryIndex, profileUuid).Bytes()
	}

	visibility, err := newEntryVisibilityResolver(s, req.ViewerUuid)
	if err != nil {
		return nil, err
	}
	feedDecision, err := visibility.feed(profile)
	if err != nil {
		return nil, err
	}
	if err := visibility.readError(feedDecision, "feed"); err != nil {
		return nil, err
	}

	slog.Info("ForwardFetchFeed", "prefix", hex.EncodeToString(prefix))

	start := req.Start
	var entries []*pb.Entry
	found := 0
	resolver := newProfileResolver(s.mdb)
	_, err = s.rdb.ForwardScan(prefix, func(i int, k, v []byte) error {
		// Direct index values are empty; the entry UUID lives in the index
		// key suffix.
		_, entryUUID, _, err := model.ParseEntryIndexKey(k)
		if err != nil {
			return err
		}
		entryKey := model.Entry.PrefixAppend(entryUUID.Bytes())
		entry := new(pb.Entry)
		rawdata, err := s.rdb.Get(entryKey)
		if errors.Is(err, store.ErrNotFound) {
			slog.Debug("user feed: entry missing", "index_key", hex.EncodeToString(k), "entry_key", hex.EncodeToString(entryKey))
			// slient delete the key from index
			s.rdb.Delete(k)
			slog.Debug("deleting", "key", hex.EncodeToString(k))
			return nil
		}
		if err != nil {
			return err
		}
		if err := proto.Unmarshal(rawdata, entry); err != nil {
			return err
		}
		decision, err := visibility.entry(entry)
		if err != nil {
			return err
		}
		if decision != visibilityAllowed {
			return nil
		}
		if start > 0 {
			start--
			return nil
		}
		if err := model.LoadEntryInteractions(s.rdb, entry); err != nil {
			return err
		}
		// slog.Debug("entry.rawBody", "id", entry.Id, "raw_body", entry.RawBody)
		if err = formatFeedEntryWithResolver(resolver, req, entry); err != nil {
			return err
		}

		entries = append(entries, entry)
		found++
		if found > int(req.PageSize) { // retain one lookahead entry for legacy paging
			return &store.Error{Msg: "ok", Code: store.StopIteration} // stop scan
		}
		return nil
	})

	if err != nil {
		slog.Debug("feed", "err", err)
		return nil, status.Errorf(codes.NotFound, "feeds not found")
	}

	feed := &pb.Feed{
		Uuid:        profile.Uuid,
		Id:          profile.Id,
		Name:        profile.Name,
		Picture:     profile.Picture,
		Type:        profile.Type,
		Private:     profile.Private,
		Description: profile.Description,
		Entries:     entries,
	}
	s.withPendingFollowRequest(feed, req.ViewerUuid)
	return feed, nil
}

type cursorFeedEntry struct {
	entry    *pb.Entry
	indexKey store.Key
}

const minimumCursorVisibilityScan = 300

// ForwardFetchFeedWithCursor pages profile feeds and user timelines from an
// opaque index position. Home timelines use TimelineIndex; profile feeds keep
// using direct EntryIndex. Legacy Start/PageSize behavior remains available to
// old callers, while cached public feeds never reach this method.
func (s *ApiServer) ForwardFetchFeedWithCursor(ctx context.Context, req *pb.FeedRequest) (*pb.Feed, error) {
	if req.PageSize <= 0 || req.PageSize >= 100 {
		req.PageSize = 50
	}

	profile, prefix, activityTimeline, err := s.cursorFeedTarget(req)
	if err != nil {
		return nil, err
	}
	visibility, err := newEntryVisibilityResolver(s, req.ViewerUuid)
	if err != nil {
		return nil, err
	}
	if !activityTimeline {
		decision, err := visibility.feed(profile)
		if err != nil {
			return nil, err
		}
		if err := visibility.readError(decision, "feed"); err != nil {
			return nil, err
		}
	}
	if activityTimeline && !isPublicFeedRequest(req) {
		viewer, parseErr := uuid.FromString(profile.Uuid)
		if parseErr != nil || viewer == uuid.Nil {
			return nil, status.Error(codes.Internal, "profile has invalid UUID")
		}
		if visibility.profile == nil || visibility.viewer != viewer {
			return nil, status.Error(codes.PermissionDenied, "home timeline is owner-only")
		}
		initializing, err := s.prepareHomeTimeline(viewer, time.Now().UTC())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "initialize home timeline: %v", err)
		}
		if initializing {
			return nil, status.Error(codes.Unavailable, pb.HomeTimelineInitializing)
		}
	}
	publicTimeline := activityTimeline && isPublicFeedRequest(req)
	cursorKey, err := decodeFeedCursor(req.Cursor, prefix)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid feed cursor: %v", err)
	}

	iter, err := s.rdb.NewIterator(prefix)
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	if len(cursorKey) > 0 {
		iter.SeekGE(cursorKey)
		// The cursor encodes the full index position, so an exact key match is
		// the previously rendered row; step past it.
		if iter.Valid() && bytes.Equal(iter.UnsafeRawKey(), cursorKey) {
			iter.Next()
		}
	} else {
		iter.First()
	}

	resolver := newProfileResolver(s.mdb)
	items := make([]cursorFeedEntry, 0, req.PageSize+1)
	visibleStart := int32(0)
	if activityTimeline && !req.CursorPaging {
		// Legacy ?start=N counts visible rows, not raw derived-index rows.
		visibleStart = req.Start
	}
	scanBudget := max(int(req.PageSize)*10, minimumCursorVisibilityScan) + int(visibleStart)
	scanned := 0
	var lastScanned store.Key
	for iter.Valid() && len(items) <= int(req.PageSize) && scanned < scanBudget {
		indexKey := iter.Key()
		lastScanned = indexKey
		scanned++
		var entryKey store.Key
		var timelineEntryUUID uuid.UUID
		var timelineViewerUUID uuid.UUID
		if activityTimeline {
			timelineViewerUUID, timelineEntryUUID, _, err = model.ParseTimelineIndexKey(indexKey)
			if err != nil {
				return nil, err
			}
			entryKey = model.Entry.PrefixAppend(timelineEntryUUID.Bytes())
		} else {
			// Direct index values are empty; the entry UUID lives in the
			// index key suffix.
			_, entryUUID, _, parseErr := model.ParseEntryIndexKey(indexKey)
			if parseErr != nil {
				return nil, parseErr
			}
			entryKey = model.Entry.PrefixAppend(entryUUID.Bytes())
		}
		entry := new(pb.Entry)
		rawdata, getErr := s.rdb.Get(entryKey)
		if getErr != nil && !errors.Is(getErr, store.ErrNotFound) {
			return nil, getErr
		}
		if errors.Is(getErr, store.ErrNotFound) {
			if activityTimeline {
				_, _, activity, parseErr := model.ParseTimelineIndexKey(indexKey)
				if parseErr != nil {
					return nil, parseErr
				}
				if deleteErr := s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
					return model.DeleteTimelinePositionBatch(batch, timelineViewerUUID, timelineEntryUUID, activity)
				}); deleteErr != nil {
					return nil, deleteErr
				}
			} else if deleteErr := s.rdb.Delete(indexKey); deleteErr != nil {
				return nil, deleteErr
			}
		} else {
			if err := proto.Unmarshal(rawdata, entry); err != nil {
				return nil, err
			}
			decision, visibilityErr := visibility.entry(entry)
			if publicTimeline {
				decision, visibilityErr = visibility.publicEntry(entry)
			}
			if visibilityErr != nil {
				return nil, visibilityErr
			}
			if decision == visibilityAllowed {
				if visibleStart > 0 {
					visibleStart--
					iter.Next()
					continue
				}
				if err := model.LoadEntryInteractions(s.rdb, entry); err != nil {
					return nil, err
				}
				if err := formatFeedEntryWithResolver(resolver, req, entry); err != nil {
					return nil, err
				}
				items = append(items, cursorFeedEntry{entry: entry, indexKey: indexKey})
			}
		}

		iter.Next()
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}

	hasExtra := len(items) > int(req.PageSize)
	if hasExtra {
		items = items[:req.PageSize]
	}
	feed := &pb.Feed{
		Uuid:        profile.Uuid,
		Id:          profile.Id,
		Name:        profile.Name,
		Picture:     profile.Picture,
		Type:        profile.Type,
		Private:     profile.Private,
		Description: profile.Description,
		Entries:     make([]*pb.Entry, len(items)),
	}
	for i := range items {
		feed.Entries[i] = items[i].entry
	}
	if len(items) > 0 {
		if hasExtra {
			feed.NextCursor = encodeFeedCursor(items[len(items)-1].indexKey, prefix)
		}
	}
	if !hasExtra && iter.Valid() && len(lastScanned) > 0 {
		feed.NextCursor = encodeFeedCursor(lastScanned, prefix)
	}
	s.withPendingFollowRequest(feed, req.ViewerUuid)
	return feed, nil
}

// prepareHomeTimeline schedules maintenance without tying it to the caller's
// RPC context. It returns initializing only when maintenance is needed and no
// stale row is available to render meanwhile.
func (s *ApiServer) prepareHomeTimeline(viewer uuid.UUID, now time.Time) (bool, error) {
	needed, err := s.homeTimelineMaintenanceNeeded(viewer, now)
	if err != nil || !needed {
		return false, err
	}
	s.scheduleHomeTimelineMaintenance(viewer, now)
	hasRows, err := s.homeTimelineHasRows(viewer)
	return !hasRows, err
}

func (s *ApiServer) scheduleHomeTimelineMaintenance(viewer uuid.UUID, now time.Time) {
	s.timelineFailureMu.Lock()
	retryAfter := s.timelineRetryAfter[viewer]
	s.timelineFailureMu.Unlock()
	if now.Before(retryAfter) {
		return
	}
	s.timelineMaintenance.DoChan(viewer.String(), func() (any, error) {
		if !s.beginBackgroundJob() {
			return nil, nil
		}
		defer s.wg.Done()

		// Another request may have completed maintenance before this call ran.
		needed, err := s.homeTimelineMaintenanceNeeded(viewer, now)
		if err != nil || !needed {
			return nil, err
		}

		select {
		case s.timelineBuildSlots <- struct{}{}:
		case <-s.done:
			return nil, nil
		}
		defer func() { <-s.timelineBuildSlots }()
		err = s.maintainHomeTimeline(viewer, now)
		s.timelineFailureMu.Lock()
		if err != nil {
			s.timelineRetryAfter[viewer] = time.Now().UTC().Add(homeTimelineFailureBackoff)
			slog.Error("home timeline maintenance failed", "viewer", viewer, "err", err)
		} else {
			delete(s.timelineRetryAfter, viewer)
		}
		s.timelineFailureMu.Unlock()
		return nil, err
	})
}

func (s *ApiServer) homeTimelineHasRows(viewer uuid.UUID) (bool, error) {
	iter, err := s.rdb.NewIterator(model.TimelineIndexPrefix(viewer))
	if err != nil {
		return false, err
	}
	defer iter.Close()
	iter.First()
	if err := iter.Error(); err != nil {
		return false, err
	}
	return iter.Valid(), nil
}

func (s *ApiServer) homeTimelineMaintenanceNeeded(viewer uuid.UUID, now time.Time) (bool, error) {
	lastAccess, err := model.TimelineLastAccess(s.rdb, viewer)
	if errors.Is(err, store.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	age := now.Sub(lastAccess)
	return age < 0 || age >= model.TimelineTouchAfter, nil
}

func (s *ApiServer) maintainHomeTimeline(viewer uuid.UUID, now time.Time) error {
	lastAccess, err := model.TimelineLastAccess(s.rdb, viewer)
	if err == nil {
		age := now.Sub(lastAccess)
		if age >= 0 && age <= model.TimelineActiveFor {
			if age >= model.TimelineTouchAfter {
				if _, err := model.TrimHomeTimeline(s.rdb, viewer, model.TimelineMaxEntries, model.TimelineRetentionMax, now); err != nil {
					return err
				}
				return model.TouchTimelineState(s.rdb, viewer, now)
			}
			return nil
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	if err := s.rebuildHomeTimelineNow(viewer, now); err != nil {
		return err
	}
	slog.Info("initialized home timeline", "viewer", viewer)
	return nil
}

func (s *ApiServer) cursorFeedTarget(req *pb.FeedRequest) (*pb.Profile, store.Key, bool, error) {
	if isPublicFeedRequest(req) {
		// The public timeline is a reserved TimelineIndex viewer, not a real
		// profile. The pseudo profile keeps the historical wire values;
		// httpd rewrites feed.Uuid == "Public" to the current user.
		profile := &pb.Profile{
			Uuid: "Public",
			Id:   "Public",
			Name: "Everyone's feed",
			Type: "group",
		}
		return profile, model.TimelineIndexPrefix(model.PublicTimelineUUID), true, nil
	}
	if req.ProfileUuid != "" {
		profileUUID, err := uuid.FromString(req.ProfileUuid)
		if err != nil {
			return nil, nil, false, status.Error(codes.InvalidArgument, "invalid profile UUID")
		}
		profile, err := model.GetProfileFromUuid(s.mdb, profileUUID)
		if err != nil {
			return nil, nil, false, status.Error(codes.NotFound, "profile not found")
		}
		return profile, model.TimelineIndexPrefix(profileUUID), true, nil
	}

	profile, err := model.GetProfileFromUserId(s.mdb, req.Id)
	if err != nil {
		profile, err = model.GetProfileFromRenameId(s.mdb, req.Id)
		if err != nil {
			return nil, nil, false, status.Error(codes.NotFound, "profile not found")
		}
	}
	profileUUID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return nil, nil, false, status.Error(codes.Internal, "profile has invalid UUID")
	}
	return profile, store.NewUUIDKey(model.TableEntryIndex, profileUUID).Bytes(), false, nil
}

// feedCursorPositionSize is the cursor payload shared by TimelineIndex and
// direct EntryIndex positions: reverse Unix ms (8 B) + entry UUID (16 B).
const feedCursorPositionSize = 8 + uuid.Size

func encodeFeedCursor(key, prefix store.Key) string {
	if !bytes.HasPrefix(key, prefix) {
		return ""
	}
	position := key[len(prefix):]
	if len(position) != feedCursorPositionSize {
		return ""
	}
	return util.Base58Encode(position)
}

func decodeFeedCursor(cursor string, prefix store.Key) (store.Key, error) {
	if cursor == "" {
		return nil, nil
	}
	position, err := util.Base58Decode(cursor)
	if err != nil {
		return nil, err
	}
	if len(position) != feedCursorPositionSize {
		return nil, errors.New("invalid cursor position")
	}
	key := make(store.Key, 0, len(prefix)+len(position))
	key = append(key, prefix...)
	key = append(key, position...)
	return key, nil
}

func (s *ApiServer) FetchEntry(ctx context.Context, req *pb.EntryRequest) (*pb.Feed, error) {
	slog.Info("FetchEntry", "uuid", req.Uuid)
	entry, err := model.GetEntry(s.rdb, req.Uuid)
	if err != nil {
		slog.Debug("FetchEntry error", "err", err)
		return nil, status.Errorf(codes.NotFound, "entry not found")
	}
	visibility, err := newEntryVisibilityResolver(s, req.ViewerUuid)
	if err != nil {
		return nil, err
	}
	decision, err := visibility.entry(entry)
	if err != nil {
		return nil, err
	}
	if err := visibilityReadError(decision, "entry"); err != nil {
		return nil, err
	}

	// slog.Debug("entry", "raw_body", entry.RawBody)
	// fmtEntryProfiles resolves the author by stable ProfileUuid, falling back
	// to From.Id for legacy entries without one, and refreshes entry.From.
	profile, err := fmtEntryProfiles(s.mdb, entry)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "profile not found")
	}

	feed := &pb.Feed{
		Uuid:        profile.Uuid,
		Id:          profile.Id,
		Name:        profile.Name,
		Type:        profile.Type,
		Private:     profile.Private,
		Description: profile.Description,
		Entries:     []*pb.Entry{entry},
	}
	return feed, nil
}

// canonicalizeEntryTo derives the denormalized To snapshot from FeedUuid, the
// field PutEntry actually indexes by, so the rendered "to" link can never
// disagree with the feed the entry landed in. Posting to the author's own
// feed clears To; any other canonical target (user, group or special)
// yields exactly one snapshot. Client-supplied To is never trusted;
// FriendFeed's multi-recipient To is not supported.
func canonicalizeEntryTo(mdb *store.Store, entry *pb.Entry, authorUUID uuid.UUID) error {
	if entry.FeedUuid == "" {
		entry.FeedUuid = entry.ProfileUuid // same default as model.PutEntry
	}
	feedUUID, err := uuid.FromString(entry.FeedUuid)
	if err != nil {
		return fmt.Errorf("entry FeedUuid is invalid: %w", err)
	}
	if feedUUID == authorUUID {
		entry.To = nil
		return nil
	}
	target, err := model.GetProfileFromUuid(mdb, feedUUID)
	if err != nil {
		return fmt.Errorf("entry target feed %s: %w", entry.FeedUuid, err)
	}
	entry.To = []*pb.Feed{{
		Uuid:    target.Uuid,
		Id:      target.Id,
		Name:    target.Name,
		Type:    target.Type,
		Picture: target.Picture,
	}}
	return nil
}

func (s *ApiServer) PostEntry(ctx context.Context, entry *pb.Entry) (*pb.Entry, error) {
	return s.postEntry(ctx, entry, false)
}

// postEntry is the single Entry write boundary. allowSystemFeedSelfPost is
// reserved for in-process Service/system-feed producers; the public RPC must
// never let a caller impersonate a Group by setting ProfileUuid == FeedUuid.
func (s *ApiServer) postEntry(ctx context.Context, entry *pb.Entry, allowSystemFeedSelfPost bool) (*pb.Entry, error) {
	if entry == nil {
		return nil, errors.New("entry is required")
	}
	s.entryLifecycleMu.Lock()
	defer s.entryLifecycleMu.Unlock()

	profileUuid, err := uuid.FromString(entry.ProfileUuid)
	if err != nil {
		return nil, err
	}
	profile, err := model.GetProfileFromUuid(s.mdb, profileUuid)
	if err != nil || profile == nil {
		return nil, err
	}
	// From is a display snapshot, but it must still be minted from the
	// canonical author Profile rather than trusted from the client.
	entry.From = &pb.Feed{
		Uuid:    profile.Uuid,
		Id:      profile.Id,
		Name:    profile.Name,
		Type:    profile.Type,
		Picture: profile.Picture,
	}
	if err := canonicalizeEntryTo(s.mdb, entry, profileUuid); err != nil {
		return nil, err
	}
	if profile.Type == "group" && (!allowSystemFeedSelfPost || entry.FeedUuid != profile.Uuid) {
		return nil, status.Error(codes.PermissionDenied, "a Group cannot act as a user principal")
	}
	if err := s.authorizeEntryPost(entry, profileUuid); err != nil {
		return nil, err
	}
	// key, err := store.PutEntry(s.rdb, entry, false) // always use false
	created, err := s.entryCreated(entry)
	if err != nil {
		return nil, err
	}
	_, err = model.PutEntryWithTimelineObserver(s.rdb, entry, s.realtimeObserverExcluding(profileUuid))
	if err != nil {
		return nil, err
	}
	if created {
		if err := s.bumpPublicTimeline(entry, nil); err != nil {
			return nil, err
		}
	}
	return entry, nil
}

// authorizeEntryPost enforces Group posting membership at the mutation
// boundary per docs/group.md: httpd's feedWritable is display-only and must
// not be the sole check. A public RPC may never use a Group as its actor;
// only trusted in-process producers such as FeedService imports may create a
// Group-authored Entry, via the private postEntry boundary.
func (s *ApiServer) authorizeEntryPost(entry *pb.Entry, authorUUID uuid.UUID) error {
	feedUUID, err := uuid.FromString(entry.FeedUuid)
	if err != nil {
		return fmt.Errorf("entry FeedUuid is invalid: %w", err)
	}
	target, err := model.GetProfileFromUuid(s.mdb, feedUUID)
	if err != nil {
		return err
	}
	if feedUUID == authorUUID {
		return nil
	}
	if target.Type != "group" {
		return status.Error(codes.PermissionDenied, "actor may not post to another user feed")
	}
	if err := model.CheckGroupAction(s.mdb, feedUUID, authorUUID, model.GroupActionPost); err != nil {
		return status.Errorf(codes.PermissionDenied, "actor may not post to this Group: %v", err)
	}
	return nil
}

// isGroupAdminOfEntryFeed reports whether actor is a Group admin of the
// Group entry.FeedUuid resolves to, per docs/group.md's moderation rule: a
// Group admin may delete any Entry posted into their Group, scoped strictly
// to entry.FeedUuid (not any other snapshot field, and never a grant to
// edit). Any lookup failure or non-Group FeedUuid resolves to false rather
// than propagating an error, since this is only ever consulted as a
// permission fallback after the author/super checks have failed.
func (s *ApiServer) isGroupAdminOfEntryFeed(entry *pb.Entry, actor uuid.UUID) bool {
	feedUUID, err := uuid.FromString(entry.FeedUuid)
	if err != nil || feedUUID == uuid.Nil {
		return false
	}
	target, err := model.GetProfileFromUuid(s.mdb, feedUUID)
	if err != nil || target.Type != "group" {
		return false
	}
	isAdmin, err := model.IsGroupAdmin(s.mdb, feedUUID, actor)
	if err != nil {
		return false
	}
	return isAdmin
}

func (s *ApiServer) PostTweet(ctx context.Context, tweet *pb.Tweet) (*pb.Entry, error) {
	if tweet.User == nil {
		return nil, errors.New("no user info")
	}
	if tweet.InReplyTo != "" {
		return nil, errors.New("reply not allowed")
	}

	if err := model.PutTweet(s.rdb, tweet); err != nil {
		return nil, err
	}

	profileUuid := model.UniqueKeyFrom("twitter", tweet.User.ScreenName)
	from := &pb.Feed{
		Uuid: profileUuid.String(),
		Id:   tweet.User.ScreenName,
		Name: tweet.User.Name,
		Type: "user",
	}

	entryUUID := model.UniqueKeyFrom("twitter", tweet.Id)
	url := "https://twitter.com/" + tweet.User.ScreenName + "/status/" + tweet.Id
	entry := &pb.Entry{
		Id:      entryUUID.String(),
		Url:     url,
		Date:    tweet.CreatedAt,
		Body:    tweet.Text,
		RawBody: tweet.Text,
		RawLink: url,
		From:    from,
		// To:         []*pb.Feed{from},
		Thumbnails: tweet.Medias,
		Via: &pb.Via{
			Name: "Twitter",
			Url:  url,
		},
		ProfileUuid: profileUuid.String(),
	}

	return s.PostEntry(ctx, entry)
}

func (s *ApiServer) DeleteEntry(ctx context.Context, req *pb.EntryRequest) (*pb.EntryRequest, error) {
	s.entryLifecycleMu.Lock()
	defer s.entryLifecycleMu.Unlock()

	entry, err := model.GetEntry(s.rdb, req.Uuid)
	if err != nil {
		return nil, err
	}
	userUuid, err := uuid.FromString(req.User)
	if err != nil {
		return nil, err
	}
	profile, err := model.GetProfileFromUuid(s.mdb, userUuid)
	if err != nil || profile == nil {
		return nil, err
	}
	// not superadmin, not creator, and not a Group admin of entry.FeedUuid
	if !profile.IsSuper && entry.ProfileUuid != req.User && !s.isGroupAdminOfEntryFeed(entry, userUuid) {
		return nil, status.Errorf(codes.PermissionDenied, "no perm")
	}
	err = model.DeleteEntry(s.rdb, req.Uuid)
	if err != nil {
		return nil, err
	}
	return req, nil
}

func (s *ApiServer) LikeEntry(ctx context.Context, req *pb.LikeRequest) (*pb.Entry, error) {
	s.entryLifecycleMu.RLock()
	defer s.entryLifecycleMu.RUnlock()

	entry, err := model.GetEntry(s.rdb, req.Entry)
	if err != nil {
		return nil, err
	}

	userUUID, err := uuid.FromString(req.User)
	if err != nil {
		return nil, err
	}
	profile, err := model.GetProfileFromUuid(s.mdb, userUUID)
	if err != nil || profile == nil {
		return nil, err
	}
	if err := s.requireEntryReadable(entry, profile.Uuid); err != nil {
		return nil, err
	}

	if req.Like {
		created, checkErr := s.likeCreated(entry, profile)
		if checkErr != nil {
			return nil, checkErr
		}
		_, entry, err = model.PutLikeWithTimelineObserver(s.rdb, profile, entry, s.realtimeObserverExcluding(userUUID))
		if err == nil && created {
			s.publishInteractionNotificationDirty(entry, userUUID)
			if bumpErr := s.bumpPublicTimeline(entry, nil); bumpErr != nil {
				return nil, bumpErr
			}
		}
	} else {
		entry, err = model.DeleteLike(s.rdb, profile, entry)
	}
	return entry, err
}

// principalFromUserUuid resolves the canonical profile for a command
// request's stable principal. user_uuid is REQUIRED: client-supplied
// actor references (comment.From, the legacy user id field) are kept on
// the wire for compatibility but are never an authorization fallback.
func (s *ApiServer) principalFromUserUuid(userUuid string) (*pb.Profile, error) {
	if userUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "user_uuid is required")
	}
	profileUUID, err := uuid.FromString(userUuid)
	if err != nil || profileUUID == uuid.Nil {
		return nil, status.Error(codes.InvalidArgument, "user_uuid is invalid")
	}
	profile, err := model.GetProfileFromUuid(s.mdb, profileUUID)
	if errors.Is(err, model.ErrNotFound) || errors.Is(err, model.ErrProfileDeleted) {
		return nil, status.Error(codes.NotFound, "profile not found")
	}
	if err != nil {
		// Real storage failures must not masquerade as a missing user.
		return nil, status.Errorf(codes.Internal, "read profile: %v", err)
	}
	if profile == nil {
		return nil, status.Error(codes.NotFound, "profile not found")
	}
	return profile, nil
}

func (s *ApiServer) CommentEntry(ctx context.Context, req *pb.CommentRequest) (*pb.Entry, error) {
	s.entryLifecycleMu.RLock()
	defer s.entryLifecycleMu.RUnlock()

	entry, err := model.GetEntry(s.rdb, req.Entry)
	if err != nil {
		return nil, err
	}

	profile, err := s.principalFromUserUuid(req.UserUuid)
	if err != nil {
		return nil, err
	}
	if err := s.requireEntryReadable(entry, profile.Uuid); err != nil {
		return nil, err
	}

	created, err := s.commentCreated(entry, req.Comment)
	if err != nil {
		return nil, err
	}
	actorUUID, err := uuid.FromString(profile.Uuid)
	if err != nil || actorUUID == uuid.Nil {
		return nil, status.Error(codes.Internal, "profile has invalid UUID")
	}
	_, entry, err = model.PutCommentWithTimelineObserver(s.rdb, profile, entry, req.Comment, s.realtimeObserverExcluding(actorUUID))
	if err != nil {
		return nil, err
	}
	if created {
		s.publishInteractionNotificationDirty(entry, actorUUID)
		if err := s.bumpPublicTimeline(entry, nil); err != nil {
			return nil, err
		}
	}
	return entry, nil
}

func (s *ApiServer) DeleteComment(ctx context.Context, req *pb.CommentDeleteRequest) (*pb.Entry, error) {
	s.entryLifecycleMu.RLock()
	defer s.entryLifecycleMu.RUnlock()

	entry, err := model.GetEntry(s.rdb, req.Entry)
	if err != nil {
		return nil, err
	}

	profile, err := s.principalFromUserUuid(req.UserUuid)
	if err != nil {
		return nil, err
	}
	if err := s.requireEntryReadable(entry, profile.Uuid); err != nil {
		return nil, err
	}

	return model.DeleteComment(s.rdb, profile, entry, req.Comment)
}

func (s *ApiServer) requireEntryReadable(entry *pb.Entry, viewerRaw string) error {
	visibility, err := newEntryVisibilityResolver(s, viewerRaw)
	if err != nil {
		return err
	}
	decision, err := visibility.entry(entry)
	if err != nil {
		return err
	}
	return visibility.readError(decision, "entry")
}

func (s *ApiServer) Search(ctx context.Context, req *pb.SearchRequest) (*pb.Feed, error) {
	slog.Debug("Search", "query", req.Query)
	if req.Start < 0 {
		req.Start = 0
	}
	if req.PageSize <= 0 || req.PageSize >= 100 {
		req.PageSize = 50
	}

	// Unusable documents (entry gone, author profile gone, no stable
	// identity) break the fixed start+PageSize alignment of later pages.
	// Rather than skipping them mid-page, delete them and restart from the
	// same Start: after deletion the raw bleve positions compact, so the
	// retried page and every following page line up again. The loop makes
	// strict progress — every round either returns, fails, or deletes at
	// least one document — so it needs no artificial round cap; request
	// cancellation is the escape hatch. A deletion failure or a transient
	// lookup error aborts the request instead of emitting a page that
	// pretends offsets are aligned.
	for {
		if err := ctx.Err(); err != nil {
			return nil, status.FromContextError(err).Err()
		}
		entries, unusable, err := s.searchPage(req)
		if err != nil {
			return nil, err
		}
		if len(unusable) == 0 {
			return &pb.Feed{
				Uuid:    "Search",
				Id:      "Search",
				Name:    "Search result",
				Type:    "group",
				Private: false,
				Entries: entries,
			}, nil
		}
		for _, id := range unusable {
			if err := search.Indexer.Delete(id); err != nil {
				return nil, status.Errorf(codes.Internal, "drop unusable search document %s: %v", id, err)
			}
		}
	}
}

// unusableSearchDoc reports whether err marks a search document that can
// never produce a displayable result, as opposed to a transient lookup
// failure.
func unusableSearchDoc(err error) bool {
	return errors.Is(err, model.ErrNotFound) ||
		errors.Is(err, model.ErrProfileDeleted) ||
		errors.Is(err, ErrInvalidEntryIdentity)
}

// searchPage fetches one page of valid entries starting at req.Start. It
// returns the IDs of unusable documents instead of skipping them; the caller
// decides whether to delete them and retry.
func (s *ApiServer) searchPage(req *pb.SearchRequest) (entries []*pb.Entry, unusable []string, err error) {
	resolver := newProfileResolver(s.mdb)
	visibility, err := newEntryVisibilityResolver(s, req.ViewerUuid)
	if err != nil {
		return nil, nil, err
	}
	from := int(req.Start)
	for len(entries) <= int(req.PageSize) {
		bReq := bleve.NewSearchRequest(bleve.NewQueryStringQuery(req.Query))
		bReq.From = from
		bReq.Size = int(req.PageSize) + 1 - len(entries)
		bReq.Highlight = bleve.NewHighlight()
		res, err := search.Indexer.Search(bReq)
		if err != nil {
			slog.Debug("Search error", "err", err)
			return nil, nil, err
		}
		if len(res.Hits) == 0 {
			break
		}
		// NOTE: res.Request is nil under bleve v2; never dereference it.
		from += len(res.Hits)
		for _, hit := range res.Hits {
			entry, getErr := model.GetEntry(s.rdb, hit.ID)
			if getErr != nil {
				if !errors.Is(getErr, model.ErrNotFound) {
					return nil, nil, status.Errorf(codes.Internal, "read entry %s: %v", hit.ID, getErr)
				}
				slog.Warn("search: entry data missing", "id", hit.ID)
				unusable = append(unusable, hit.ID)
				continue
			}
			// slog.Debug("entry.rawBody", "id", entry.Id, "raw_body", entry.RawBody)
			if _, fmtErr := fmtEntryProfilesWithResolver(resolver, entry); fmtErr != nil {
				if !unusableSearchDoc(fmtErr) {
					return nil, nil, status.Errorf(codes.Internal, "format entry %s: %v", hit.ID, fmtErr)
				}
				slog.Warn("search: entry can never be displayed", "id", hit.ID, "err", fmtErr)
				unusable = append(unusable, hit.ID)
				continue
			}
			decision, visErr := visibility.entry(entry)
			if visErr != nil {
				return nil, nil, visErr
			}
			switch decision {
			case visibilityDenied:
				// Keep the document: another viewer may be allowed to see it.
				continue
			case visibilityTargetUnavailable:
				unusable = append(unusable, hit.ID)
				continue
			}
			entries = append(entries, entry)
		}
		if len(res.Hits) < bReq.Size {
			break
		}
	}
	return entries, unusable, nil
}
