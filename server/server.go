package server

import (
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofrs/uuid"
	"github.com/golang/protobuf/proto"
	"github.com/sirupsen/logrus"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/model"
	pb "github.com/yinhm/friendfeed/proto"
	"github.com/yinhm/friendfeed/search"
	store "github.com/yinhm/friendfeed/storage"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
)

var logger *logrus.Logger

// server implementation.
type ApiServer struct {
	sync.RWMutex

	// meta database
	mdb *store.Store
	// block database
	rdb *store.Store
	// file system
	fs media.Storage

	// cached feed
	cached map[string]*FeedIndex
}

func init() {
	logger = logrus.StandardLogger()
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetOutput(os.Stdout)
	logrus.SetFormatter(&logrus.TextFormatter{
		ForceColors:     true,
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
		DisableSorting:  true,
	})
	grpclog.SetLogger(logger)
}

// SetLevel sets the standard logger level.
func SetLogLevel(level logrus.Level) {
	logrus.SetLevel(level)
}

func SetLogFile(f *os.File) {
	logrus.SetOutput(io.MultiWriter(f, os.Stdout))
}

func NewApiServer(dbpath, mediaConfigFile string) *ApiServer {
	rdb := store.NewStore(dbpath)
	// mdb := store.NewMetaStore(dbpath + "/meta")
	mdb := rdb

	cached := make(map[string]*FeedIndex)
	uuid1 := uuid.NewV5(uuid.NamespaceURL, "index:public:cache")
	cached["public"] = NewFeedIndex(rdb, "public", uuid1)
	cached["public"].load(mdb)

	srv := &ApiServer{
		mdb:    mdb,
		rdb:    rdb,
		cached: cached,
	}

	config, err := media.NewConfigFromJSON(mediaConfigFile)
	if err != nil {
		log.Fatal("no config file")
	}
	// TODO: fix lazy hack for local dev.
	// if no key file then go google storage.
	if _, err := os.Stat(config.KeyFile); err == nil {
		srv.fs = media.NewGoogleStorage(config)
	} else {
		srv.fs = media.NewLocalStorage(config)
	}

	return srv
}

func (s *ApiServer) Shutdown() {
	if s.rdb == nil {
		return // already closed
	}

	idx := s.cached["public"]
	logger.Debug("dump index to db...")
	idx.dump(s.mdb)
	idx.doneCh <- struct{}{}

	s.rdb.Close()
	s.mdb.Close()
	s.rdb = nil
	s.mdb = nil
}

func (s *ApiServer) Destroy() {
	logger.Warn("Destroy db...")
	s.rdb.Destroy()
	s.mdb.Destroy()
}

func (s *ApiServer) FetchFeedinfo(ctx context.Context, req *pb.ProfileRequest) (*pb.Feedinfo, error) {
	if req.Uuid == "" {
		return nil, fmt.Errorf("bad request")
	}
	return model.GetFeedinfo(s.rdb, req.Uuid)
}

func (s *ApiServer) PostFeedinfo(ctx context.Context, in *pb.Feedinfo) (*pb.Profile, error) {
	profile := &pb.Profile{
		Uuid:        in.Uuid,
		Id:          in.Id,
		Name:        in.Name,
		Type:        in.Type,
		Private:     in.Private,
		SupId:       in.SupId,
		Description: in.Description,
	}
	// remote key only present when id == target_id
	if in.RemoteKey != "" {
		// record remote key
		profile.RemoteKey = in.RemoteKey
	}

	// profile.Picture = s.ArchiveProfilePicture(profile.Id)
	// log.Println("profile pic:", profile.Picture)

	if err := model.UpdateProfile(s.mdb, profile); err != nil {
		return nil, err
	}

	// save all feed info in one key for simplicity
	// TODO: refactor?
	in.Entries = []*pb.Entry{}
	if err := model.PutFeedinfo(s.rdb, profile.Uuid, in); err != nil {
		return nil, err
	}

	// TODO: server overload, disable friends of feed
	// There is no way we can handle this much jobs in a short time.
	// remote key only present when id == target_id
	// if in.RemoteKey != "" {
	// 	for _, sub := range in.Subscriptions {
	// 		// enqueue user subscriptons
	// 		oldjob, err := store.GetArchiveHistory(s.mdb, sub.Id)
	// 		if err != nil || oldjob.Status == "done" {
	// 			// no aggressive archiving for friends of feed
	// 			log.Printf("%s previous archived.", sub.Id)
	// 			continue
	// 		}

	// 		ctx := context.Background()
	// 		key := store.NewFlakeKey(store.TableJobFeed, s.mdb.NextId())
	// 		job := &pb.FeedJob{
	// 			Key:       key.String(),
	// 			Id:        in.Id,
	// 			RemoteKey: in.RemoteKey,
	// 			TargetId:  sub.Id,
	// 			Start:     0,
	// 			PageSize:  100,
	// 			Created:   time.Now().Unix(),
	// 			Updated:   time.Now().Unix(),
	// 		}
	// 		s.EnqueJob(ctx, job)
	// 	}
	// }

	return profile, nil
}

// TODO: build graph if it not exists
func (s *ApiServer) FetchGraph(ctx context.Context, req *pb.ProfileRequest) (*pb.Graph, error) {
	if req.Uuid == "" {
		return nil, fmt.Errorf("bad request")
	}
	feedinfo, err := model.GetFeedinfo(s.rdb, req.Uuid)
	if err != nil {
		return nil, err
	}
	return BuildGraph(feedinfo), nil
}

func (s *ApiServer) ArchiveProfilePicture(id string) string {
	url := fmt.Sprintf("http://friendfeed-api.com/v2/picture/%s?size=large", id)
	ok, picUrl, _ := CheckRedirect(url)
	if !ok {
		log.Printf("retrieve %s's picture failed.", id)
		return ""
	}

	newObj, err := s.fs.FromUrl("", picUrl, "")
	if err != nil {
		log.Println("Mirror media failed:", err)
		return picUrl
	}
	return newObj.Url
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
			endTime := time.Now()
			return stream.SendAndClose(&pb.FeedSummary{
				EntryCount:  entryCount,
				DateStart:   dateStart,
				DateEnd:     dateEnd,
				ElapsedTime: int32(endTime.Sub(startTime).Seconds()),
			})
		}
		if err != nil {
			return err
		}
		entryCount++
		// key, err := store.PutEntry(s.rdb, entry, false) // always use false
		key, err := model.PutEntry(s.rdb, entry)
		if err == nil {
			// no error or new key
			s.spread(key.String())
		}
		// Retuen if not force update and all entries are exists
		// TODO: client dead lock???
		if serr, ok := err.(*store.Error); ok {
			if serr.Code == store.ExistItem {
				err = nil
				// tooMuchExistsItem++
				// if tooMuchExistsItem > 200 {
				// 	return fmt.Errorf("Too much exists entries.")
				// }
			}
		}
		if err != nil {
			log.Println("db error:", err)
		}

		go s.mirrorMedia(s.fs, entry)
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
			endTime := time.Now()
			return stream.SendAndClose(&pb.FeedSummary{
				EntryCount:  entryCount,
				DateStart:   dateStart,
				DateEnd:     dateEnd,
				ElapsedTime: int32(endTime.Sub(startTime).Seconds()),
			})
		}
		if err != nil {
			return err
		}
		entryCount++
		// save db
		key, err := model.PutEntry(s.rdb, entry)
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

func (s *ApiServer) mirrorMedia(client media.Storage, entry *pb.Entry) error {
	// twitpic should be fine, see: http://blog.twitpic.com/2014/10/twitpics-future/
	for _, thumb := range entry.Thumbnails {
		newObj, err := client.FromUrl("", thumb.Url, "")
		if err != nil {
			// log.Println("Mirror media failed:", err)
			continue
		}
		thumb.Url = newObj.Url // rewrote to mirrored

		newObj, err = client.FromUrl("", thumb.Link, "")
		if err != nil {
			// log.Println("Mirror media failed:", err)
			continue
		}
	}

	for _, file := range entry.Files {
		newObj, err := client.FromUrl(file.Name, file.Url, file.Type)
		if err != nil {
			// log.Println("Mirror media failed:", err)
			continue
		}
		file.Url = newObj.Url // rewrote to mirrored
	}
	return nil
}

// FetchFeed returns builded feed which populated data
// from user profile and entries scaned from EntryIndex.
func (s *ApiServer) FetchFeed(ctx context.Context, req *pb.FeedRequest) (*pb.Feed, error) {
	logger.Infof("FetchFeed: %s", req.Id)
	s.RLock()
	if _, ok := s.cached[req.Id]; ok {
		s.RUnlock()
		logger.Debugf("cachedFeed: %s", req.Id)
		return s.cachedFeed(req)
	}
	s.RUnlock()
	return s.ForwardFetchFeed(ctx, req)
}

func (s *ApiServer) cachedFeed(req *pb.FeedRequest) (*pb.Feed, error) {
	if req.PageSize <= 0 || req.PageSize >= 100 {
		req.PageSize = 50
	}

	start := req.Start
	index := s.cached[req.Id]

	var entries []*pb.Entry
	found := 0
	for i := 0; i < len(index.bufq); i++ {
		if start > 0 {
			start--
			continue
		}

		key := index.bufq[i]
		if key == "" {
			break
		}

		kb, _ := hex.DecodeString(key)
		// logger.Debugf("index.key: <%s>", key)
		entry := new(pb.Entry)
		rawdata, err := s.rdb.Get(kb)
		if err != nil || len(rawdata) == 0 {
			logger.Warnf("cachedFeed: entry data missing: %s", req.Id)
			logger.Warn("FIXME: rebuild index system")
			continue
		}
		if err := proto.Unmarshal(rawdata, entry); err != nil {
			return nil, err
		}
		// logger.Debugf("entry.rawBody: <%s, %s>", entry.Id, entry.RawBody)
		FormatFeedEntry(s.mdb, req, entry)
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
		SupId:   "0000-00",
		Entries: entries[:],
	}
	return feed, nil
}

func (s *ApiServer) ForwardFetchFeed(ctx context.Context, req *pb.FeedRequest) (*pb.Feed, error) {
	if req.PageSize <= 0 || req.PageSize >= 100 {
		req.PageSize = 50
	}

	profile, err := model.GetProfileFromUserId(s.mdb, req.Id)
	if err != nil {
		logger.Debugf("ForwardFetchFeed: %s, err: %s", req.Id, err)
		return nil, status.Errorf(codes.NotFound, "profile not found")
	}

	uuid1, _ := uuid.FromString(profile.Uuid)
	preKey := store.NewUUIDKey(model.TableEntryIndex, uuid1)
	logger.Infof("ForwardFetchFeed: %s", preKey.String())

	start := req.Start
	var entries []*pb.Entry
	_, err = s.rdb.ForwardScan(preKey, func(i int, k, v []byte) error {
		if start > 0 {
			start--
			return nil // continue
		}

		entry := new(pb.Entry)
		rawdata, err := s.rdb.Get(v) // index value point to entry key
		if err != nil || len(rawdata) == 0 {
			logger.Warnf("entry missing %s", string(v))
			// slient delete the key from index
			s.rdb.Delete(k)
			return nil
		}
		if err := proto.Unmarshal(rawdata, entry); err != nil {
			return err
		}
		if err = FormatFeedEntry(s.mdb, req, entry); err != nil {
			return err
		}

		entries = append(entries, entry)
		if i > int(req.PageSize+req.Start) {
			return &store.Error{Msg: "ok", Code: store.StopIteration} // stop scan
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	feed := &pb.Feed{
		Uuid:        profile.Uuid,
		Id:          profile.Id,
		Name:        profile.Name,
		Picture:     profile.Picture,
		Type:        profile.Type,
		Private:     profile.Private,
		SupId:       profile.SupId,
		Description: profile.Description,
		Entries:     entries[:],
	}
	return feed, nil
}

func (s *ApiServer) FetchEntry(ctx context.Context, req *pb.EntryRequest) (*pb.Feed, error) {
	logger.Infof("FetchEntry: %s", req.Uuid)
	entry, err := model.GetEntry(s.rdb, req.Uuid)
	if err != nil {
		logger.Debug(err)
		return nil, err
	}
	// logger.Debugf("entry: %s", entry.RawBody)
	err = fmtEntryProfile(s.mdb, entry)
	if err != nil {
		return nil, err
	}

	profile, err := model.GetProfileFromUserId(s.mdb, entry.From.Id)
	if err != nil || profile == nil {
		return nil, status.Errorf(codes.NotFound, "profile not found")
	}

	feed := &pb.Feed{
		Uuid:        profile.Uuid,
		Id:          profile.Id,
		Name:        profile.Name,
		Type:        profile.Type,
		Private:     profile.Private,
		SupId:       profile.SupId,
		Description: profile.Description,
		Entries:     []*pb.Entry{entry},
	}
	return feed, nil
}

func (s *ApiServer) PostEntry(ctx context.Context, entry *pb.Entry) (*pb.Entry, error) {
	// key, err := store.PutEntry(s.rdb, entry, false) // always use false
	key, err := model.PutEntry(s.rdb, entry) // always use false
	if err != nil {
		return nil, err
	}
	s.spread(key.String())
	return entry, nil
}

func (s *ApiServer) DeleteEntry(ctx context.Context, req *pb.EntryRequest) (*pb.EntryRequest, error) {
	entry, err := model.GetEntry(s.rdb, req.Uuid)
	if err != nil {
		return nil, err
	}
	if entry.ProfileUuid != req.User {
		return nil, status.Errorf(codes.PermissionDenied, "no perm")
	}
	err = model.DeleteEntry(s.rdb, req.Uuid)
	if err != nil {
		return nil, err
	}
	return req, nil
}

func (s *ApiServer) LikeEntry(ctx context.Context, req *pb.LikeRequest) (*pb.Entry, error) {
	entry, err := model.GetEntry(s.rdb, req.Entry)
	if err != nil {
		return nil, err
	}

	uuid1, err := uuid.FromString(req.User)
	if err != nil {
		return nil, err
	}
	profile, err := model.GetProfileFromUuid(s.mdb, uuid1)
	if err != nil || profile == nil {
		return nil, err
	}

	if req.Like {
		var key store.Key
		key, entry, err = model.Like(s.rdb, profile, entry)
		if err == nil {
			s.spread(key.String())
		}
	} else {
		entry, err = model.DeleteLike(s.rdb, profile, entry)
	}
	return entry, err
}

func (s *ApiServer) CommentEntry(ctx context.Context, req *pb.CommentRequest) (*pb.Entry, error) {
	entry, err := model.GetEntry(s.rdb, req.Entry)
	if err != nil {
		return nil, err
	}

	profile, err := model.GetProfileFromUserId(s.mdb, req.Comment.From.Id)
	if err != nil || profile == nil {
		return nil, err
	}

	key, entry, err := model.Comment(s.rdb, profile, entry, req.Comment)
	if err != nil {
		return nil, err
	}
	s.spread(key.String())
	return entry, nil
}

func (s *ApiServer) DeleteComment(ctx context.Context, req *pb.CommentDeleteRequest) (*pb.Entry, error) {
	entry, err := model.GetEntry(s.rdb, req.Entry)
	if err != nil {
		return nil, err
	}

	profile, err := model.GetProfileFromUserId(s.mdb, req.User)
	if err != nil || profile == nil {
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
	logger.Infof("Search: %s", req.Query)
	bReq := bleve.NewSearchRequest(bleve.NewQueryStringQuery(req.Query))
	bReq.Highlight = bleve.NewHighlight()
	res, err := search.Indexer.Search(bReq)
	if err != nil {
		logger.Debug(err)
		return nil, err
	}

	start := req.Start

	var entries []*pb.Entry
	found := 0
	for i, hit := range res.Hits {
		if start > 0 {
			start--
			continue
		}

		rv := fmt.Sprintf("%d. %s, (%f)\n", i+res.Request.From+1, hit.ID, hit.Score)
		// for fragmentField, fragments := range hit.Fragments {
		// 	rv += fmt.Sprintf("%s: ", fragmentField)
		// 	for _, fragment := range fragments {
		// 		rv += fmt.Sprintf("%s", fragment)
		// 	}
		// }
		fmt.Printf("%s\n", rv)

		logger.Debugf("search.index.key: <%s>", hit.ID)
		entry := new(pb.Entry)
		entry, err := model.GetEntry(s.rdb, hit.ID)
		if err != nil {
			logger.Warnf("search: entry data missing: %s", hit.ID)
			continue
		}
		// logger.Debugf("entry.rawBody: <%s, %s>", entry.Id, entry.RawBody)
		if err := fmtEntryProfile(s.mdb, entry); err != nil {
			logger.Warnf("search: entry format error: %s", hit.ID)
			continue
		}
		entries = append(entries, entry)
		found++
		if found > int(req.PageSize) {
			break
		}
	}

	feed := &pb.Feed{
		Uuid:    "Search",
		Id:      "Search",
		Name:    "Search result",
		Type:    "group",
		Private: false,
		Entries: entries[:],
	}
	return feed, nil
}
