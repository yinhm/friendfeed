package server

import (
	"encoding/hex"
	"errors"
	"math/rand"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func FormatFeedEntry(mdb *store.Store, req *pb.FeedRequest, entry *pb.Entry) error {
	if _, err := fmtEntryProfile(mdb, entry); err != nil {
		return err
	}
	// fmtComments(req, entry)
	// fmtLikes(req, entry)
	return nil
}

// fmtEntryProfile resolves the entry author profile and refreshes the
// denormalized entry.From snapshot from it. Returns the resolved profile.
func fmtEntryProfile(mdb *store.Store, entry *pb.Entry) (*pb.Profile, error) {
	// Refetch the author profile. Resolve by the stable ProfileUuid, NOT by
	// the denormalized From.Id: From.Id is a snapshot taken when the entry
	// was posted and goes stale if the author later renames their profile
	// ID. Resolving by id would then fail and 404 the entire feed.
	var profile *pb.Profile
	var err error
	if entry.ProfileUuid != "" {
		profileUUID, uerr := uuid.FromString(entry.ProfileUuid)
		if uerr != nil {
			return nil, uerr
		}
		profile, err = model.GetProfileFromUuid(mdb, profileUUID)
	} else if entry.From != nil {
		// Legacy entries without ProfileUuid fall back to id lookup.
		profile, err = model.GetProfileFromUserId(mdb, entry.From.Id)
	} else {
		return nil, errors.New("entry has neither ProfileUuid nor From")
	}
	if err != nil {
		return nil, err
	}

	if entry.From == nil {
		entry.From = &pb.Feed{}
	}
	// Refresh denormalized fields from the canonical profile so a renamed ID,
	// updated name or picture render correctly for historical entries.
	entry.From.Id = profile.Id
	entry.From.Name = profile.Name
	entry.From.Picture = profile.Picture
	return profile, nil
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
