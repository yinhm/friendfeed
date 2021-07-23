package server

import (
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
