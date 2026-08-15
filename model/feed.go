package model

import (
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

// Obsoleted, Feedinfo are virtual now
func PutFeedinfo(db *store.Store, uuidStr string, info *pb.Feedinfo) error {
	feedUUID, err := uuid.FromString(uuidStr)
	if err != nil {
		return err
	}

	_, err = Feedinfo.Put(db, feedUUID.Bytes(), info)
	return err
}

// Obsoleted, Feedinfo are virtual now
func GetFeedinfo(db *store.Store, uuidStr string) (*pb.Feedinfo, error) {
	feedUUID, err := uuid.FromString(uuidStr)
	if err != nil {
		return nil, err
	}

	info := new(pb.Feedinfo)
	err = Feedinfo.Get(db, feedUUID.Bytes(), info)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func GetArchiveHistory(db *store.Store, id string) (*pb.FeedJob, error) {
	job := new(pb.FeedJob)
	err := JobHistory.Get(db, []byte(id), job)
	if err != nil {
		return nil, err
	}
	return job, nil
}

func PutFeedService(db *store.Store, profileUuid uuid.UUID, service *pb.FeedService) error {
	key := NewKeyFrom(profileUuid.Bytes(), []byte(service.Id))
	_, err := FeedService.Put(db, key, service)
	return err
}

func DeleteFeedService(db *store.Store, profileUuid uuid.UUID, serviceId string) error {
	key := NewKeyFrom(profileUuid.Bytes(), []byte(serviceId))
	return FeedService.Delete(db, key)
}

func GetFeedServices(db *store.Store, profileUuid uuid.UUID) ([]*pb.FeedService, error) {
	prefix := NewPrefixKeyFrom(TableFeedService, profileUuid.Bytes())
	// fmt.Printf("scan key, %s\n", prefix.String())

	var services []*pb.FeedService
	_, err := db.ForwardScan(prefix, func(i int, k, v []byte) error {
		service := &pb.FeedService{}
		if err := proto.Unmarshal(v, service); err != nil {
			return err
		}
		services = append(services, service)
		return nil
	})
	return services, err
}
