package server

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func (s *ApiServer) exportTwitterUsersTSV() (string, error) {
	var out bytes.Buffer
	w := csv.NewWriter(&out)
	w.Comma = '\t'
	if err := w.Write([]string{"feed_id", "feed_uuid", "twitter_username", "twitter_user_id", "boundary_tweet_id", "boundary_at"}); err != nil {
		return "", err
	}
	err := model.OAuth.Iter(s.mdb, func(_, raw []byte) error {
		oauth := new(pb.OAuthUser)
		if err := proto.Unmarshal(raw, oauth); err != nil {
			return err
		}
		if !strings.EqualFold(oauth.Provider, "twitter") || oauth.Uuid == "" || oauth.UserId == "" || oauth.Name == "" {
			return nil
		}
		feedUUID, err := uuid.FromString(oauth.Uuid)
		if err != nil {
			return fmt.Errorf("Twitter OAuth %s has invalid Feed UUID: %w", oauth.UserId, err)
		}
		profile, err := model.GetProfileFromUuid(s.mdb, feedUUID)
		if errors.Is(err, model.ErrNotFound) || errors.Is(err, model.ErrProfileDeleted) {
			return nil
		}
		if err != nil {
			return err
		}
		itemID, publishedAt, err := latestTwitterBoundary(s.rdb, feedUUID)
		if err != nil {
			return err
		}
		return w.Write([]string{profile.Id, profile.Uuid, oauth.Name, oauth.UserId, itemID, publishedAt})
	})
	w.Flush()
	if err != nil {
		return "", err
	}
	if err := w.Error(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func latestTwitterBoundary(db *store.Store, feed uuid.UUID) (string, string, error) {
	prefix := model.NewUUIDKey(model.TableEntryIndex, feed)
	iter, err := db.NewIterator(prefix)
	if err != nil {
		return "", "", err
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		_, entryUUID, _, err := model.ParseEntryIndexKey(iter.Key())
		if err != nil {
			return "", "", err
		}
		entry, err := model.GetEntry(db, entryUUID.String())
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		values := []string{entry.RawLink, entry.Url}
		if entry.Via != nil {
			values = append(values, entry.Via.Url)
		}
		for _, raw := range values {
			if itemID, ok := twitterItemIDFromURL(raw); ok {
				published, err := time.Parse(time.RFC3339Nano, entry.Date)
				if err != nil {
					return "", "", err
				}
				return itemID, published.UTC().Format(time.RFC3339Nano), nil
			}
		}
	}
	if err := iter.Error(); err != nil {
		return "", "", err
	}
	return "", "", nil
}
