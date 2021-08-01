package model

import (
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	store "github.com/yinhm/friendfeed/storage"
)

func PutFeedinfo(db *store.Store, uuidStr string, info *pb.Feedinfo) error {
	uuid1, err := uuid.FromString(uuidStr)
	if err != nil {
		return err
	}

	_, err = Feedinfo.Put(db, uuid1.Bytes(), info)
	return err
}

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
