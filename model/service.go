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

const WebFeedServiceKind = "web_feed"

func NormalizeServiceURL(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("parse service URL: %w", err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("service URL must use http or https")
	}
	if parsed.User != nil {
		return "", errors.New("service URL must not contain userinfo")
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return "", errors.New("service URL host is required")
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
	// RawQuery is intentionally preserved byte-for-byte: unlike fragments,
	// query order and escaping may be part of a feed endpoint's identity.
	return parsed.String(), nil
}

func ServiceIdentity(kind, rawURL string) (string, uuid.UUID, error) {
	if kind != WebFeedServiceKind {
		return "", uuid.Nil, fmt.Errorf("unsupported service kind %q", kind)
	}
	normalized, err := NormalizeServiceURL(rawURL)
	if err != nil {
		return "", uuid.Nil, err
	}
	return normalized, UniqueKeyFrom("service", kind, normalized), nil
}

func GetService(db *store.Store, serviceID uuid.UUID) (*pb.Service, error) {
	if serviceID == uuid.Nil {
		return nil, errors.New("service UUID is zero")
	}
	result := new(pb.Service)
	if err := Service.Get(db, serviceID.Bytes(), result); err != nil {
		return nil, err
	}
	return result, nil
}

func GetServiceState(db *store.Store, serviceID uuid.UUID) (*pb.ServiceState, error) {
	if serviceID == uuid.Nil {
		return nil, errors.New("service UUID is zero")
	}
	result := new(pb.ServiceState)
	if err := ServiceState.Get(db, serviceID.Bytes(), result); err != nil {
		return nil, err
	}
	return result, nil
}

func PutServiceState(db *store.Store, serviceID uuid.UUID, state *pb.ServiceState) error {
	if serviceID == uuid.Nil || state == nil || state.ServiceUuid != serviceID.String() {
		return errors.New("ServiceState identity mismatch")
	}
	_, err := ServiceState.Put(db, serviceID.Bytes(), state)
	return err
}

func FeedServiceKey(target uuid.UUID, serviceID string) (store.Key, error) {
	if target == uuid.Nil || serviceID == "" {
		return nil, errors.New("target Feed UUID and service ID are required")
	}
	return NewKeyFrom(FeedService.Prefix, target.Bytes(), []byte(serviceID)), nil
}

func ServiceFeedIndexKey(service, target uuid.UUID, serviceID string) (store.Key, error) {
	if service == uuid.Nil || target == uuid.Nil || serviceID == "" {
		return nil, errors.New("service UUID, target Feed UUID, and service ID are required")
	}
	return NewKeyFrom(ServiceFeedIndex.Prefix, service.Bytes(), target.Bytes(), []byte(serviceID)), nil
}

func GetFeedService(db *store.Store, target uuid.UUID, serviceID string) (*pb.FeedService, error) {
	key, err := FeedServiceKey(target, serviceID)
	if err != nil {
		return nil, err
	}
	result := new(pb.FeedService)
	if err := FeedService.Get(db, FeedService.PrefixRemove(key), result); err != nil {
		return nil, err
	}
	return result, nil
}

func ListFeedServices(db *store.Store, target uuid.UUID) ([]*pb.FeedService, error) {
	if target == uuid.Nil {
		return nil, errors.New("target Feed UUID is zero")
	}
	prefix := NewKeyFrom(FeedService.Prefix, target.Bytes())
	result := make([]*pb.FeedService, 0)
	_, err := db.ForwardScan(prefix, func(_ int, _, value []byte) error {
		item := new(pb.FeedService)
		if err := proto.Unmarshal(value, item); err != nil {
			return err
		}
		result = append(result, item)
		return nil
	})
	return result, err
}

type ServiceFeedBinding struct {
	TargetFeedUUID uuid.UUID
	ServiceID      string
}

func ListServiceFeedBindings(db *store.Store, serviceID uuid.UUID) ([]ServiceFeedBinding, error) {
	if serviceID == uuid.Nil {
		return nil, errors.New("service UUID is zero")
	}
	prefix := NewKeyFrom(ServiceFeedIndex.Prefix, serviceID.Bytes())
	result := make([]ServiceFeedBinding, 0)
	_, err := db.ForwardScan(prefix, func(_ int, key, _ []byte) error {
		suffix := key[len(prefix):]
		if len(suffix) <= uuid.Size {
			return fmt.Errorf("invalid ServiceFeedIndex key length %d", len(key))
		}
		target, err := uuid.FromBytes(suffix[:uuid.Size])
		if err != nil {
			return err
		}
		result = append(result, ServiceFeedBinding{TargetFeedUUID: target, ServiceID: string(suffix[uuid.Size:])})
		return nil
	})
	return result, err
}

func StageAddWebFeedService(db *store.Store, batch *pebble.Batch, target, actor uuid.UUID, rawURL string, now time.Time) (*pb.FeedService, *pb.Service, error) {
	if batch == nil || target == uuid.Nil || actor == uuid.Nil {
		return nil, nil, errors.New("batch, target Feed UUID, and actor UUID are required")
	}
	if now.IsZero() || now.UnixMilli() < 0 {
		return nil, nil, errors.New("service creation time is invalid")
	}
	normalized, serviceUUID, err := ServiceIdentity(WebFeedServiceKind, rawURL)
	if err != nil {
		return nil, nil, err
	}
	parsed, _ := url.Parse(normalized)
	name := strings.ToLower(parsed.Hostname())
	service := &pb.Service{
		Uuid: serviceUUID.String(), Kind: WebFeedServiceKind, CanonicalUrl: normalized,
		Title: name, SiteUrl: parsed.Scheme + "://" + parsed.Host, CreatedAtMs: now.UTC().UnixMilli(), UpdatedAtMs: now.UTC().UnixMilli(),
	}
	if raw, getErr := Service.GetRaw(db, serviceUUID.Bytes()); getErr == nil {
		existing := new(pb.Service)
		if err := proto.Unmarshal(raw, existing); err != nil {
			return nil, nil, fmt.Errorf("decode Service %s: %w", serviceUUID, err)
		}
		if existing.Uuid != serviceUUID.String() || existing.Kind != WebFeedServiceKind || existing.CanonicalUrl != normalized {
			return nil, nil, fmt.Errorf("Service %s identity mismatch", serviceUUID)
		}
		service = existing
	} else if !errors.Is(getErr, store.ErrNotFound) {
		return nil, nil, getErr
	} else if err := setProto(batch, Service.PrefixAppend(serviceUUID.Bytes()), service); err != nil {
		return nil, nil, err
	}

	state := &pb.ServiceState{ServiceUuid: serviceUUID.String(), NextFetchMs: now.UTC().UnixMilli()}
	if _, getErr := ServiceState.GetRaw(db, serviceUUID.Bytes()); errors.Is(getErr, store.ErrNotFound) {
		if err := setProto(batch, ServiceState.PrefixAppend(serviceUUID.Bytes()), state); err != nil {
			return nil, nil, err
		}
	} else if getErr != nil {
		return nil, nil, getErr
	}

	feedService := &pb.FeedService{
		Id: serviceUUID.String(), Kind: WebFeedServiceKind, ServiceUuid: serviceUUID.String(),
		Name: name, Profile: normalized, Enabled: true, AddedByUuid: actor.String(), Created: now.UTC().Unix(), Updated: now.UTC().Unix(),
	}
	feedKey, _ := FeedServiceKey(target, feedService.Id)
	if raw, getErr := db.Get(feedKey); getErr == nil {
		existing := new(pb.FeedService)
		if err := proto.Unmarshal(raw, existing); err != nil {
			return nil, nil, fmt.Errorf("decode FeedService %s/%s: %w", target, feedService.Id, err)
		}
		if existing.ServiceUuid != serviceUUID.String() {
			return nil, nil, fmt.Errorf("FeedService %s/%s identity mismatch", target, feedService.Id)
		}
		feedService = existing
	} else if !errors.Is(getErr, store.ErrNotFound) {
		return nil, nil, getErr
	} else if err := setProto(batch, feedKey, feedService); err != nil {
		return nil, nil, err
	}
	indexKey, _ := ServiceFeedIndexKey(serviceUUID, target, feedService.Id)
	if err := batch.Set(indexKey, nil, nil); err != nil {
		return nil, nil, err
	}
	return feedService, service, nil
}

func StageRemoveFeedService(db *store.Store, batch *pebble.Batch, target uuid.UUID, serviceID string) error {
	if batch == nil {
		return errors.New("batch is required")
	}
	key, err := FeedServiceKey(target, serviceID)
	if err != nil {
		return err
	}
	raw, err := db.Get(key)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	binding := new(pb.FeedService)
	if err := proto.Unmarshal(raw, binding); err != nil {
		return fmt.Errorf("decode FeedService %s/%s: %w", target, serviceID, err)
	}
	if binding.ServiceUuid != "" {
		serviceUUID, err := uuid.FromString(binding.ServiceUuid)
		if err != nil || serviceUUID == uuid.Nil {
			return fmt.Errorf("FeedService %s/%s has invalid service UUID", target, serviceID)
		}
		indexKey, _ := ServiceFeedIndexKey(serviceUUID, target, serviceID)
		if err := batch.Delete(indexKey, nil); err != nil {
			return err
		}
	}
	return batch.Delete(key, nil)
}

func StageSetFeedServiceEnabled(db *store.Store, batch *pebble.Batch, target uuid.UUID, serviceID string, enabled bool) (*pb.FeedService, error) {
	binding, err := GetFeedService(db, target, serviceID)
	if err != nil {
		return nil, err
	}
	serviceUUID, err := uuid.FromString(binding.ServiceUuid)
	if err != nil {
		return nil, fmt.Errorf("FeedService %q has no canonical Service: %w", serviceID, err)
	}
	binding.Enabled = enabled
	data, err := proto.Marshal(binding)
	if err != nil {
		return nil, err
	}
	bindingKey, err := FeedServiceKey(target, serviceID)
	if err != nil {
		return nil, err
	}
	if err := batch.Set(bindingKey, data, nil); err != nil {
		return nil, err
	}
	indexKey, err := ServiceFeedIndexKey(serviceUUID, target, serviceID)
	if err != nil {
		return nil, err
	}
	if enabled {
		err = batch.Set(indexKey, nil, nil)
	} else {
		err = batch.Delete(indexKey, nil)
	}
	if err != nil {
		return nil, err
	}
	return binding, nil
}

func setProto(batch *pebble.Batch, key store.Key, message proto.Message) error {
	raw, err := proto.Marshal(message)
	if err != nil {
		return err
	}
	return batch.Set(key, raw, nil)
}
