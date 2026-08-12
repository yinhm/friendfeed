package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/store/flake"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// server implementation.
type ApiServer struct {
	sync.RWMutex
	profileUpdateMu sync.Mutex
	jobMu           sync.Mutex
	// entryLifecycleMu lets independent interaction rows mutate concurrently,
	// while keeping them mutually exclusive with Entry create/edit/delete.
	entryLifecycleMu sync.RWMutex

	// meta database
	mdb *store.Store
	// block database
	rdb *store.Store
	// file system
	fs media.Storage

	// cached feed
	cached map[string]*FeedIndex

	// background job lifecycle: done signals shutdown, wg tracks
	// running job goroutines so Shutdown can wait for them before
	// closing the database.
	done chan struct{}
	wg   sync.WaitGroup

	lifecycleMu           sync.Mutex
	backgroundJobsStarted bool
	shuttingDown          bool
	shutdownOnce          sync.Once
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

	cached := make(map[string]*FeedIndex)
	publicIndexUUID := uuid.NewV5(uuid.NamespaceURL, "index:public:cache")
	cached["public"] = NewFeedIndex(rdb, "public", publicIndexUUID)
	cached["public"].load(mdb)

	srv := &ApiServer{
		mdb:    mdb,
		rdb:    rdb,
		cached: cached,
		done:   make(chan struct{}),
	}

	srv.fs = media.NewStorage(cfg, 1024)
	return srv, nil
}

// StartBackgroundJobs starts the periodic refetch and index dump jobs.
// They are stopped by Shutdown before the database is closed.
func (s *ApiServer) StartBackgroundJobs() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.backgroundJobsStarted || s.shuttingDown {
		return
	}
	s.backgroundJobsStarted = true
	go s.RefetchJobTicker()
	go s.IndexJobTicker()
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
		// Prevent new background jobs from starting, then wait for existing
		// jobs to finish using the database.
		s.lifecycleMu.Lock()
		s.shuttingDown = true
		close(s.done)
		s.lifecycleMu.Unlock()
		s.wg.Wait()

		idx := s.cached["public"]
		idx.Stop()
		slog.Debug("dump index to db...")
		idx.dump(s.mdb)

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
	// Keep the read/rename/patch sequence together. RenameProfileId makes
	// its key changes atomic; this lock prevents a concurrent profile
	// update from writing an older snapshot back after that atomic commit.
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
	ss, _ := model.GetServicesForProfile(s.rdb, profileUuid)
	feedinfo.Services = ss

	return BuildGraph(feedinfo), nil
}

func (s *ApiServer) ArchiveFeed(stream pb.Api_ArchiveFeedServer) error {
	var entryCount int32
	var dateStart string
	var dateEnd string
	var lastEntry *pb.Entry
	startTime := time.Now()

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
		s.entryLifecycleMu.Lock()
		key, err := model.PutEntry(s.rdb, entry)
		s.entryLifecycleMu.Unlock()
		if err == nil {
			// no error or new key
			s.spread(key.String())
		}
		if err != nil {
			log.Println("db error:", err)
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
		s.entryLifecycleMu.Lock()
		key, err := model.PutEntry(s.rdb, entry)
		s.entryLifecycleMu.Unlock()
		if err != nil {
			log.Println("db error:", err)
		} else {
			// TODO: spread?
			s.cached["public"].Push(key.String())
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
	s.RLock()
	if _, ok := s.cached[req.Id]; ok {
		s.RUnlock()
		slog.Debug("cachedFeed", "id", req.Id)
		return s.cachedFeed(req)
	}
	s.RUnlock()
	if req.CursorPaging || req.ProfileUuid != "" {
		return s.ForwardFetchFeedWithCursor(ctx, req)
	}
	return s.ForwardFetchFeed(ctx, req)
}

func (s *ApiServer) cachedFeed(req *pb.FeedRequest) (*pb.Feed, error) {
	if req.PageSize <= 0 || req.PageSize >= 100 {
		req.PageSize = 50
	}

	start := req.Start
	index := s.cached[req.Id]
	bufq := index.snapshot()

	var entries []*pb.Entry
	found := 0
	resolver := newProfileResolver(s.mdb)
	for i := range bufq {
		if start > 0 {
			start--
			continue
		}

		key := bufq[i]
		if key == "" {
			break
		}

		kb, _ := hex.DecodeString(key)
		// slog.Debug("index.key", "key", key)
		entry := new(pb.Entry)
		rawdata, err := s.rdb.Get(kb)
		if errors.Is(err, store.ErrNotFound) {
			slog.Warn("index cached: data missing", "id", req.Id, "key", key)
			s.cached[req.Id].markDirty()
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := proto.Unmarshal(rawdata, entry); err != nil {
			return nil, err
		}
		if err := model.LoadEntryInteractions(s.rdb, entry); err != nil {
			return nil, err
		}
		// slog.Debug("entry.rawBody", "id", entry.Id, "raw_body", entry.RawBody)
		_ = formatFeedEntryWithResolver(resolver, req, entry)
		entries = append(entries, entry)
		found++
		if found > int(req.PageSize) {
			break
		}
	}

	feed := &pb.Feed{
		Uuid:    "Public",
		Id:      "Public",
		Name:    "Everyone's feed",
		Type:    "group",
		Private: false,
		Entries: entries,
	}
	return feed, nil
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
	slog.Info("ForwardFetchFeed", "prefix", hex.EncodeToString(prefix))

	start := req.Start
	var entries []*pb.Entry
	found := 0
	resolver := newProfileResolver(s.mdb)
	_, err = s.rdb.ForwardScan(prefix, func(i int, k, v []byte) error {
		if start > 0 {
			start--
			return nil // continue
		}

		// slog.Debug("entry key", "index_key", hex.EncodeToString(k), "entry_key", hex.EncodeToString(v))
		entry := new(pb.Entry)
		rawdata, err := s.rdb.Get(v) // index value point to entry key
		if errors.Is(err, store.ErrNotFound) {
			slog.Debug("user feed: entry missing", "index_key", hex.EncodeToString(k), "entry_key", hex.EncodeToString(v))
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
	return feed, nil
}

type cursorFeedEntry struct {
	entry    *pb.Entry
	indexKey store.Key
}

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
		if iter.Valid() && bytes.Equal(iter.UnsafeRawKey(), cursorKey) {
			iter.Next()
		}
	} else {
		iter.First()
		// Home links generated before cursor pagination used ?start=N. Keep
		// them useful by applying the offset to the new TimelineIndex rather
		// than falling back to the retired EntryIndex timeline.
		if req.ProfileUuid != "" && !req.CursorPaging {
			for skipped := int32(0); iter.Valid() && skipped < req.Start; skipped++ {
				iter.Next()
			}
		}
	}

	resolver := newProfileResolver(s.mdb)
	items := make([]cursorFeedEntry, 0, req.PageSize+1)
	for iter.Valid() && len(items) <= int(req.PageSize) {
		indexKey := iter.Key()
		entryKey := iter.Value()
		var timelineEntryUUID uuid.UUID
		var timelineViewerUUID uuid.UUID
		if activityTimeline {
			timelineViewerUUID, timelineEntryUUID, _, err = model.ParseTimelineIndexKey(indexKey)
			if err != nil {
				return nil, err
			}
			entryKey = model.Entry.PrefixAppend(timelineEntryUUID.Bytes())
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
			if err := model.LoadEntryInteractions(s.rdb, entry); err != nil {
				return nil, err
			}
			if err := formatFeedEntryWithResolver(resolver, req, entry); err != nil {
				return nil, err
			}
			items = append(items, cursorFeedEntry{entry: entry, indexKey: indexKey})
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
	return feed, nil
}

func (s *ApiServer) cursorFeedTarget(req *pb.FeedRequest) (*pb.Profile, store.Key, bool, error) {
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

func encodeFeedCursor(key, prefix store.Key) string {
	positionSize := feedCursorPositionSize(prefix)
	if len(key) != len(prefix)+positionSize || !bytes.HasPrefix(key, prefix) {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(key[len(prefix):])
}

func decodeFeedCursor(cursor string, prefix store.Key) (store.Key, error) {
	if cursor == "" {
		return nil, nil
	}
	position, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, err
	}
	positionSize := feedCursorPositionSize(prefix)
	if len(position) != positionSize {
		return nil, errors.New("invalid cursor position")
	}
	key := make(store.Key, 0, len(prefix)+len(position))
	key = append(key, prefix...)
	key = append(key, position...)
	return key, nil
}

func feedCursorPositionSize(prefix store.Key) int {
	if bytes.Equal(prefix[:model.TimelineIndex.Prefix.Len()], model.TimelineIndex.Prefix) {
		return 8 + uuid.Size
	}
	var flakeID flake.Id
	return len(flakeID) + model.Entry.Prefix.Len() + uuid.Size
}

func (s *ApiServer) FetchEntry(ctx context.Context, req *pb.EntryRequest) (*pb.Feed, error) {
	slog.Info("FetchEntry", "uuid", req.Uuid)
	entry, err := model.GetEntry(s.rdb, req.Uuid)
	if err != nil {
		slog.Debug("FetchEntry error", "err", err)
		return nil, status.Errorf(codes.NotFound, "entry not found")
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
	if entry.From == nil {
		entry.From = &pb.Feed{
			Id:   profile.Id,
			Name: profile.Name,
			Type: profile.Type,
		}
	}
	if err := canonicalizeEntryTo(s.mdb, entry, profileUuid); err != nil {
		return nil, err
	}
	// key, err := store.PutEntry(s.rdb, entry, false) // always use false
	key, err := model.PutEntry(s.rdb, entry) // always use false
	if err != nil {
		return nil, err
	}
	s.spread(key.String())
	return entry, nil
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
	// not superadmin and not creator
	if !profile.IsSuper && entry.ProfileUuid != req.User {
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

	if req.Like {
		var key store.Key
		key, entry, err = model.PutLike(s.rdb, profile, entry)
		if err == nil {
			s.spread(key.String())
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

	key, entry, err := model.PutComment(s.rdb, profile, entry, req.Comment)
	if err != nil {
		return nil, err
	}
	s.spread(key.String())
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

	return model.DeleteComment(s.rdb, profile, entry, req.Comment)
}

func (s *ApiServer) spread(key string) {
	if key != "" {
		s.cached["public"].Push(key)
	}
	// TODO: spread to friends?
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
			entries = append(entries, entry)
		}
		if len(res.Hits) < bReq.Size {
			break
		}
	}
	return entries, unusable, nil
}
