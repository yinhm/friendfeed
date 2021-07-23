package server

import (
	"math/rand"

	"github.com/golang/protobuf/proto"
	"github.com/yinhm/friendfeed/model"
	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
)

func FormatFeedEntry(mdb *store.Store, req *pb.FeedRequest, entry *pb.Entry) error {
	if err := fmtEntryProfile(mdb, entry); err != nil {
		return err
	}
	// fmtComments(req, entry)
	// fmtLikes(req, entry)
	return nil
}

func FormatEntry(mdb *store.Store, req *pb.FeedRequest, entry *pb.Entry) error {
	if err := fmtEntryProfile(mdb, entry); err != nil {
		return err
	}
	// fmtLikes(req, entry)
	return nil
}

func fmtEntryProfile(mdb *store.Store, entry *pb.Entry) error {
	// refetch user profile
	profile, err := model.GetProfileFromUserId(mdb, entry.From.Id)
	if err != nil {
		return err
	}
	entry.From.Picture = profile.Picture
	return nil
}

func fmtComments(req *pb.FeedRequest, entry *pb.Entry) {
	entry.FormatComments(req.MaxComments)
}

func fmtLikes(req *pb.FeedRequest, entry *pb.Entry) {
	entry.FormatLikes(req.MaxLikes)
}

func BuildGraph(info *pb.Feedinfo) *pb.Graph {
	graph := &pb.Graph{
		Subscriptions: make(map[string]*pb.Profile),
		Admins:        make(map[string]*pb.Profile),
		Services:      make(map[string]*pb.Service),
	}
	// FIXME: subscriptions may huge
	// for _, item := range info.Subscriptions {
	// 	graph.Subscriptions[item.Id] = item
	// }
	for _, item := range info.Admins {
		graph.Admins[item.Id] = item
	}
	for _, item := range info.Services {
		graph.Services[item.Id] = item
	}
	return graph
}

func RandomPictureFromWallpaper(db *store.Store, profile *pb.Profile) string {
	uuid1 := model.UniqueKeyFrom("bing", "wallpaper")
	profile, err := model.GetProfileFromUuid(db, uuid1)
	if err != nil {
		logger.Debugf("RandomPictureFromWallpaper: %s", err)
		return ""
	}

	// no update if not empty
	if profile.Picture != "" {
		return profile.Picture
	}

	preKey := model.NewUUIDKey(model.TableEntryIndex, uuid1)
	logger.Infof("ForwardFetchFeed: %s", preKey.String())

	url := ""
	_, err = db.ForwardScan(preKey, func(i int, k, v []byte) error {
		// logger.Debugf("entry key: <%x>", v)
		entry := new(pb.Entry)
		rawdata, err := db.Get(v) // index value point to entry key
		if err != nil || len(rawdata) == 0 {
			return nil
		}
		if err := proto.Unmarshal(rawdata, entry); err != nil {
			return err
		}

		dice := rand.Intn(10)
		if 5 == dice {
			url = entry.Thumbnails[0].Url
			return &store.Error{Msg: "ok", Code: store.StopIteration} // stop scan
		}
		return nil
	})

	return url
}
