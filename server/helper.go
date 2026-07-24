package server

import (
	"encoding/hex"
	"math/rand"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func FormatFeedEntry(mdb *store.Store, req *pb.FeedRequest, entry *pb.Entry) error {
	if err := fmtEntryProfile(mdb, entry); err != nil {
		return err
	}
	// fmtComments(req, entry)
	// fmtLikes(req, entry)
	return nil
}

func fmtEntryProfile(mdb *store.Store, entry *pb.Entry) error {
	// refetch user profile
	var err error
	var profile *pb.Profile
	if entry.From != nil {
		profile, err = model.GetProfileFromUserId(mdb, entry.From.Id)
		if err != nil {
			return err
		}
	} else {
		profileUUID, err := uuid.FromString(entry.ProfileUuid)
		if err != nil {
			return err
		}
		profile, err = model.GetProfileFromUuid(mdb, profileUUID)
		if err != nil {
			return err
		}
		entry.From = &pb.Feed{Id: profile.Id}
	}
	entry.From.Picture = profile.Picture
	return nil
}

func BuildGraph(info *pb.Feedinfo) *pb.Graph {
	graph := &pb.Graph{
		Following: make(map[string]*pb.Profile),
		Admins:    make(map[string]*pb.Profile),
		Services:  make(map[string]*pb.Service),
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
	profileUUID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		logger.Debugf("RandomPictureFromWallpaper: %s", err)
		return ""
	}
	profile, err = model.GetProfileFromUuid(db, profileUUID)
	if err != nil {
		logger.Debugf("RandomPictureFromWallpaper, no profile: %s", err)
		return ""
	}

	// no update if not empty
	if profile.Picture != "" {
		return profile.Picture
	}

	bingUuid := model.UniqueKeyFrom("bing", "wallpaper")
	preKey := model.NewUUIDKey(model.TableEntryIndex, bingUuid)
	logger.Infof("RandomPictureFromWallpaper: %s", preKey.String())

	url := ""
	_, _ = db.ForwardScan(preKey, func(i int, k, v []byte) error {
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
		logger.Debugf("entry key: <%x, dice: %d>", hex.EncodeToString(k), dice)
		if dice == 5 && len(entry.Thumbnails) > 0 && entry.Thumbnails[0] != nil {
			url = entry.Thumbnails[0].Url
			return &store.Error{Msg: "ok", Code: store.StopIteration} // stop scan
		}
		return nil
	})

	return url
}
