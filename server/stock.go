package server

import (
	"bytes"
	"encoding/gob"
	"io"
	"time"

	"github.com/yinhm/friendfeed/model"
	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
)

func (s *ApiServer) ArchiveKLine(stream pb.Api_ArchiveKLineServer) error {
	var count int32
	var dateStart int32
	var dateEnd int32
	startTime := time.Now()

	// disable pebble.Sync since we cannot do batch easily
	// and sync was too slow for large data.
	// diable it so we can do 100K/1s records via stream.
	s.rdb.SetSync(false)
	defer s.rdb.SetSync(true)

	for {
		kReq, err := stream.Recv()
		if err == io.EOF {
			endTime := time.Now()
			return stream.SendAndClose(&pb.ArchiveSummary{
				Count:       count,
				DateStart:   dateStart,
				DateEnd:     dateEnd,
				ElapsedTime: int32(endTime.Sub(startTime).Seconds()),
			})
		}
		if err != nil {
			return err
		}
		count++

		oldtime := time.Unix(int64(kReq.KLine.Date), 0)
		flakeid := s.rdb.TimeTravelReverseId(oldtime)
		uuid1 := model.UniqueKeyFrom(kReq.Market, kReq.Symbol)
		k := store.NewUUIDFlakeKey(model.TableKLine, uuid1, flakeid)
		_, err = model.KLine.Put(s.rdb, k.Bytes(), kReq.KLine)
		if err != nil {
			return err
		}

		if dateStart == 0 {
			dateStart = kReq.KLine.Date
		}
		dateEnd = kReq.KLine.Date
	}
}

// XRXD 除权除息同步为全量更新
func (s *ApiServer) ArchiveXRXD(stream pb.Api_ArchiveXRXDServer) error {
	var count int32
	startTime := time.Now()

	// disable pebble.Sync since we cannot do batch easily
	// and sync was too slow for large data.
	// diable it so we can do 100K/1s records via stream.
	s.rdb.SetSync(false)
	defer s.rdb.SetSync(true)

	dividends := make(map[string][]*pb.XRXD)

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			// 存储除权除息信息
			for k, v := range dividends {
				// use gob for []*xrxd encoding
				var buf bytes.Buffer
				enc := gob.NewEncoder(&buf)
				err := enc.Encode(v)
				if err != nil {
					return err
				}
				err = s.rdb.Put([]byte(k), buf.Bytes())
				if err != nil {
					return err
				}
			}

			endTime := time.Now()
			return stream.SendAndClose(&pb.ArchiveSummary{
				Count:       count,
				ElapsedTime: int32(endTime.Sub(startTime).Seconds()),
			})
		}
		if err != nil {
			return err
		}
		count++

		keyStr := model.KeyFrom(req.Market, req.Symbol, "xdxr")
		keyStr = model.NewPrefixKeyFrom(model.TableStock, []byte(keyStr)).String()
		dividends[keyStr] = append(dividends[keyStr], req)
	}
}
