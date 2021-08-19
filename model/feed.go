package model

import (
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

// Obsoleted, Feedinfo are virtual now
func PutFeedinfo(db *store.Store, uuidStr string, info *pb.Feedinfo) error {
	uuid1, err := uuid.FromString(uuidStr)
	if err != nil {
		return err
	}

	_, err = Feedinfo.Put(db, uuid1.Bytes(), info)
	return err
}

// Obsoleted, Feedinfo are virtual now
func GetFeedinfo(db *store.Store, uuidStr string) (*pb.Feedinfo, error) {
	uuid1, err := uuid.FromString(uuidStr)
	if err != nil {
		return nil, err
	}

	info := new(pb.Feedinfo)
	err = Feedinfo.Get(db, uuid1.Bytes(), info)
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

func PutService(db *store.Store, profileUuid uuid.UUID, service *pb.Service) error {
	key := NewKeyFrom(profileUuid.Bytes(), []byte(service.Id))
	_, err := Service.Put(db, key, service)
	return err
}

func DeleteService(db *store.Store, profileUuid uuid.UUID, serviceId string) error {
	key := NewKeyFrom(profileUuid.Bytes(), []byte(serviceId))
	return Service.Delete(db, key)
}

func GetServicesForProfile(db *store.Store, profileUuid uuid.UUID) ([]*pb.Service, error) {
	prefix := NewPrefixKeyFrom(TableService, profileUuid.Bytes())
	// fmt.Printf("scan key, %s\n", prefix.String())

	var services []*pb.Service
	_, err := db.ForwardScan(prefix, func(i int, k, v []byte) error {
		service := &pb.Service{}
		if err := proto.Unmarshal(v, service); err != nil {
			return err
		}
		services = append(services, service)
		return nil
	})
	return services, err
}
