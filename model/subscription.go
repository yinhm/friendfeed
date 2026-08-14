package model

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func NormalizeSubscriptionURL(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("parse subscription URL: %w", err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("subscription URL must use http or https")
	}
	if parsed.User != nil {
		return "", errors.New("subscription URL must not contain userinfo")
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return "", errors.New("subscription URL host is required")
	}
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	parsed.RawQuery = parsed.Query().Encode()
	return parsed.String(), nil
}

func SubscriptionIdentity(rawURL string) (string, uuid.UUID, error) {
	normalized, err := NormalizeSubscriptionURL(rawURL)
	if err != nil {
		return "", uuid.Nil, err
	}
	return normalized, UniqueKeyFrom("rss", normalized), nil
}

func SubscribeRSS(db *store.Store, subscriber uuid.UUID, rawURL string, now time.Time) (*pb.Subscription, error) {
	if subscriber == uuid.Nil {
		return nil, errors.New("subscriber UUID is zero")
	}
	if _, err := GetProfileFromUuid(db, subscriber); err != nil {
		return nil, fmt.Errorf("resolve subscriber: %w", err)
	}
	normalized, feedID, err := SubscriptionIdentity(rawURL)
	if err != nil {
		return nil, err
	}
	if now.IsZero() || now.UnixMilli() < 0 {
		return nil, errors.New("subscription time is invalid")
	}
	now = now.UTC()
	profileID := "rss-" + strings.ReplaceAll(feedID.String(), "-", "")[:16]
	if _, err := FindProfileRenameByOldId(db, profileID); err == nil {
		return nil, fmt.Errorf("synthetic profile ID %q is reserved", profileID)
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("parse normalized subscription URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	subscription := &pb.Subscription{
		FeedUuid: feedID.String(), Url: normalized, Title: host,
		AddedBy: subscriber.String(), CreatedAtMs: now.UnixMilli(),
	}
	profile := &pb.Profile{
		Uuid: feedID.String(), Id: profileID, Name: host, Type: "feed",
		Description: normalized,
	}
	state := &pb.SubscriptionState{FeedUuid: feedID.String(), NextFetchMs: now.UnixMilli()}

	err = db.ApplyBatch(func(batch *pebble.Batch) error {
		if raw, getErr := Subscription.GetRaw(db, feedID.Bytes()); getErr == nil {
			existing := new(pb.Subscription)
			if err := proto.Unmarshal(raw, existing); err != nil {
				return fmt.Errorf("decode Subscription %s: %w", feedID, err)
			}
			if existing.Url != normalized {
				return fmt.Errorf("Subscription %s URL mismatch", feedID)
			}
			subscription = existing
		} else if !errors.Is(getErr, store.ErrNotFound) {
			return getErr
		} else if err := setProto(batch, Subscription.PrefixAppend(feedID.Bytes()), subscription); err != nil {
			return err
		}

		if _, getErr := SubscriptionState.GetRaw(db, feedID.Bytes()); errors.Is(getErr, store.ErrNotFound) {
			if err := setProto(batch, SubscriptionState.PrefixAppend(feedID.Bytes()), state); err != nil {
				return err
			}
		} else if getErr != nil {
			return getErr
		}

		if raw, getErr := Profile.GetRaw(db, feedID.Bytes()); getErr == nil {
			existing := new(pb.Profile)
			if err := proto.Unmarshal(raw, existing); err != nil {
				return fmt.Errorf("decode synthetic Profile %s: %w", feedID, err)
			}
			if existing.Uuid != feedID.String() || existing.Type != "feed" {
				return fmt.Errorf("synthetic Profile %s conflicts with existing profile", feedID)
			}
		} else if !errors.Is(getErr, store.ErrNotFound) {
			return getErr
		} else {
			if err := setProto(batch, Profile.PrefixAppend(feedID.Bytes()), profile); err != nil {
				return err
			}
		}
		userMapKey := NewKeyFrom(UserMap.Prefix, []byte(profileID))
		if mapped, mapErr := db.Get(userMapKey); mapErr == nil && string(mapped) != string(feedID.Bytes()) {
			return fmt.Errorf("synthetic profile ID %q is already mapped", profileID)
		} else if mapErr != nil && !errors.Is(mapErr, store.ErrNotFound) {
			return mapErr
		}
		if err := batch.Set(userMapKey, feedID.Bytes(), nil); err != nil {
			return err
		}

		followKey := NewKeyFrom(Follow.Prefix, subscriber.Bytes(), feedID.Bytes())
		followerKey := NewKeyFrom(Follower.Prefix, feedID.Bytes(), subscriber.Bytes())
		if err := batch.Set(followKey, []byte("1"), nil); err != nil {
			return err
		}
		return batch.Set(followerKey, []byte("1"), nil)
	})
	if err != nil {
		return nil, err
	}
	return subscription, nil
}

func UnsubscribeRSS(db *store.Store, subscriber, feedID uuid.UUID) error {
	if subscriber == uuid.Nil || feedID == uuid.Nil {
		return errors.New("subscriber and feed UUIDs are required")
	}
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		if err := batch.Delete(NewKeyFrom(Follow.Prefix, subscriber.Bytes(), feedID.Bytes()), nil); err != nil {
			return err
		}
		return batch.Delete(NewKeyFrom(Follower.Prefix, feedID.Bytes(), subscriber.Bytes()), nil)
	})
}

func ListRSSSubscriptions(db *store.Store, subscriber uuid.UUID) ([]*pb.Subscription, error) {
	if subscriber == uuid.Nil {
		return nil, errors.New("subscriber UUID is zero")
	}
	prefix := NewKeyFrom(Follow.Prefix, subscriber.Bytes())
	result := make([]*pb.Subscription, 0)
	_, err := db.ForwardScan(prefix, func(_ int, key, _ []byte) error {
		if len(key) != prefix.Len()+uuid.Size {
			return fmt.Errorf("invalid Follow key length %d", len(key))
		}
		feedID, err := uuid.FromBytes(key[prefix.Len():])
		if err != nil {
			return err
		}
		subscription := new(pb.Subscription)
		if err := Subscription.Get(db, feedID.Bytes(), subscription); errors.Is(err, ErrNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		result = append(result, subscription)
		return nil
	})
	return result, err
}

func GetSubscription(db *store.Store, feedID uuid.UUID) (*pb.Subscription, error) {
	result := new(pb.Subscription)
	if err := Subscription.Get(db, feedID.Bytes(), result); err != nil {
		return nil, err
	}
	return result, nil
}

func GetSubscriptionState(db *store.Store, feedID uuid.UUID) (*pb.SubscriptionState, error) {
	result := new(pb.SubscriptionState)
	if err := SubscriptionState.Get(db, feedID.Bytes(), result); err != nil {
		return nil, err
	}
	return result, nil
}

func PutSubscriptionState(db *store.Store, feedID uuid.UUID, state *pb.SubscriptionState) error {
	if feedID == uuid.Nil || state == nil || state.FeedUuid != feedID.String() {
		return errors.New("SubscriptionState identity mismatch")
	}
	_, err := SubscriptionState.Put(db, feedID.Bytes(), state)
	return err
}

func SubscriptionHasFollowers(db *store.Store, feedID uuid.UUID) (bool, error) {
	prefix := NewKeyFrom(Follower.Prefix, feedID.Bytes())
	found := false
	_, err := db.ForwardScan(prefix, func(int, []byte, []byte) error {
		found = true
		return &store.Error{Code: store.StopIteration, Msg: "follower found"}
	})
	return found, err
}

func setProto(batch *pebble.Batch, key store.Key, message proto.Message) error {
	raw, err := proto.Marshal(message)
	if err != nil {
		return err
	}
	return batch.Set(key, raw, nil)
}
