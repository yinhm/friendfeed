package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

// legacyDefaultPictureHost/Path identify the retired FriendFeed default
// avatar; any scheme and any ?v= query variant match.
const (
	legacyDefaultPictureHost = "friendfeed.com"
	legacyDefaultPicturePath = "/static/images/group-large.png"

	// defaultPictureReplacement mirrors httpd/src.DefaultPictureURL; kept as a
	// literal so cli/tools does not import the web server package.
	defaultPictureReplacement = "/static/images/ff-default.jpg"
)

type defaultPictureFixStats struct {
	profiles int
	fixed    int
}

func isLegacyDefaultPicture(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return parsed.Host == legacyDefaultPictureHost && parsed.Path == legacyDefaultPicturePath
}

// fixDefaultPictures rewrites the retired FriendFeed default avatar stored on
// Profiles to the local fallback image. Streaming, one record at a time.
func fixDefaultPictures(db *store.Store, user string, dryRun bool) (defaultPictureFixStats, error) {
	stats := defaultPictureFixStats{}
	err := model.Profile.Iter(db, func(key, raw []byte) error {
		profile := new(pb.Profile)
		if err := proto.Unmarshal(raw, profile); err != nil {
			return fmt.Errorf("decode profile at %x: %w", key, err)
		}
		if user != "" && profile.Id != user && profile.Uuid != user {
			return nil
		}
		stats.profiles++
		if !isLegacyDefaultPicture(profile.Picture) {
			return nil
		}
		stats.fixed++
		if dryRun {
			return nil
		}
		profile.Picture = defaultPictureReplacement
		encoded, err := proto.Marshal(profile)
		if err != nil {
			return fmt.Errorf("encode profile %q: %w", profile.Id, err)
		}
		if err := db.Set(key, encoded); err != nil {
			return fmt.Errorf("write profile %q: %w", profile.Id, err)
		}
		return nil
	})
	return stats, err
}
