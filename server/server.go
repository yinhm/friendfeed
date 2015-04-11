package server

import (
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	uuid "github.com/satori/go.uuid"
	"github.com/yinhm/friendfeed/media"
	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
	"golang.org/x/net/context"
)

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

func NewApiServer(dbpath, mediaConfigFile string) *ApiServer {
	rdb := store.NewStore(dbpath)
	mdb := store.NewMetaStore(dbpath + "/meta")

	cached := make(map[string]*FeedIndex)
	cached["public"] = NewFeedIndex("public", new(uuid.UUID))
	cached["public"].load(mdb)

	srv := &ApiServer{
		mdb:    mdb,
		rdb:    rdb,
		cached: cached,
	}

	config, err := media.NewConfigFromJSON(mediaConfigFile)
	if err != nil {
		log.Fatal("no media config file")
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
	idx.dump(s.mdb)
	idx.doneCh <- struct{}{}

	s.rdb.Close()
	s.mdb.Close()
	s.rdb = nil
	s.mdb = nil
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

	if err := store.UpdateProfile(s.mdb, profile); err != nil {
		return nil, err
	}

	// save all feed info in one key for simplicity
	// TODO: refactor?
	in.Entries = []*pb.Entry{}
	if err := store.SaveFeedinfo(s.rdb, profile.Uuid, in); err != nil {
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
		// save db
		key, err := store.PutEntry(s.rdb, entry, false)
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
		} else {
			s.cached["public"].Push(key.String())
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
		key, err := store.PutEntry(s.rdb, entry, true)
		if err != nil {
			log.Println("db error:", err)
		} else {
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

func (s *ApiServer) FetchFeed(ctx context.Context, req *pb.FeedRequest) (*pb.Feed, error) {
	s.RLock()
	if _, ok := s.cached[req.Id]; ok {
		s.RUnlock()
		return s.cachedFeed(req)
	}
	s.RUnlock()
	// return s.BackwardFetchFeed(ctx, req)
	return s.ForwardFetchFeed(ctx, req)
}

func (s *ApiServer) cachedFeed(req *pb.FeedRequest) (*pb.Feed, error) {
	if req.PageSize <= 0 || req.PageSize >= 100 {
		req.PageSize = 50
	}

	start := req.Start
	index := s.cached[req.Id]

	var entries []*pb.Entry
	for i := 0; i < len(index.bufq); {
		if start > 0 {
			start--
			continue
		}

		key := index.bufq[i]
		if key == "" {
			break
		}

		kb, _ := hex.DecodeString(key)
		entry := new(pb.Entry)
		rawdata, err := s.rdb.Get(kb)
		if err != nil {
			return nil, err
		}
		if err := proto.Unmarshal(rawdata, entry); err != nil {
			return nil, err
		}
		s.formatEntry(req, entry)
		entries = append(entries, entry)
		i++
		if i > int(req.PageSize) {
			break
		}
	}

	feed := &pb.Feed{
		Uuid:    "public",
		Id:      "public",
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

	profile, err := store.GetProfile(s.mdb, req.Id)
	if err != nil {
		return nil, err
	}

	uuid1, _ := uuid.FromString(profile.Uuid)
	preKey := store.NewUUIDKey(store.TableReverseEntryIndex, uuid1)
	log.Println("forward seeking:", preKey.String())

	start := req.Start
	var entries []*pb.Entry
	_, err = store.ForwardTableScan(s.rdb, preKey, func(i int, k, v []byte) error {
		if start > 0 {
			start--
			return nil // continue
		}

		entry := new(pb.Entry)
		rawdata, err := s.rdb.Get(v) // index value point to entry key
		if err != nil {
			return err
		}
		if err := proto.Unmarshal(rawdata, entry); err != nil {
			return err
		}
		if err = s.formatEntry(req, entry); err != nil {
			return err
		}

		entries = append(entries, entry)
		if i > int(req.PageSize+req.Start) {
			return &store.Error{"ok", store.StopIteration}
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

func (s *ApiServer) formatEntry(req *pb.FeedRequest, entry *pb.Entry) error {
	// refetch user profile
	profile, err := store.GetProfile(s.mdb, entry.From.Id)
	if err != nil {
		return err
	}
	entry.From.Picture = profile.Picture

	// collapse commetns and likes
	length := len(entry.Comments)
	if req.MaxComments == 0 && length > 4 {
		collapsing := &pb.Comment{
			Body:        fmt.Sprintf("%d more comments", length-2),
			Num:         int32(length - 2),
			Placeholder: true,
		}
		entry.Comments = []*pb.Comment{entry.Comments[0], collapsing, entry.Comments[length-1]}
	}
	length = len(entry.Likes)
	if req.MaxLikes == 0 && length > 4 {
		collapsing := &pb.Like{
			Body:        fmt.Sprintf("%d other people", length-2),
			Num:         int32(length - 2),
			Placeholder: true,
		}
		entry.Likes = entry.Likes[:3]
		entry.Likes = append(entry.Likes, collapsing)
	}

	return nil
}

func (s *ApiServer) FetchEntry(ctx context.Context, req *pb.EntryRequest) (*pb.Feed, error) {
	entry, err := store.GetEntry(s.rdb, req.Uuid)
	if err != nil {
		return nil, err
	}

	profile, err := store.GetProfile(s.mdb, entry.From.Id)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("404")
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

func (s *ApiServer) PostEntry(ctx context.Context, in *pb.Entry) (*pb.Entry, error) {
	return nil, fmt.Errorf("Not implemented")
}

func (s *ApiServer) LikeEntry(ctx context.Context, req *pb.LikeRequest) (*pb.Entry, error) {
	entry, err := store.GetEntry(s.rdb, req.Entry)
	if err != nil {
		return nil, err
	}

	uuid1, err := uuid.FromString(req.User)
	if err != nil {
		return nil, err
	}
	profile, err := store.GetProfileFromUuid(s.mdb, uuid1)
	if err != nil || profile == nil {
		return nil, err
	}

	if req.Like {
		store.Like(s.rdb, profile, entry)
	} else {
		store.DeleteLike(s.rdb, profile, entry)
	}

	return entry, nil
}
