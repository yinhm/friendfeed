package server

import (
	"bytes"
	"encoding/gob"
	"io"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/yinhm/friendfeed/model"
	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
	"golang.org/x/net/context"
)

func (s *ApiServer) ArchiveKLine(stream pb.Api_ArchiveKLineServer) error {
	logger.Debugln("Starting ArchiveKLine...")
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

		// Stock KLine:
		// ------------------------------------------------
		// K-> | TableKLine |   uuid   | reverse flake id |
		// ------------------------------------------------
		// V-> |                   <data>                 |
		// ------------------------------------------------
		oldtime := time.Unix(int64(kReq.KLine.Date), 0)
		flakeid := s.rdb.TimeTravelReverseId(oldtime)
		uuid1 := model.UniqueKeyFrom(kReq.Market, kReq.Symbol)
		k := model.NewKeyFrom(uuid1.Bytes(), flakeid[:])
		_, err = model.KLine.Put(s.rdb, k, kReq.KLine)
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
	logger.Debugln("Starting ArchiveXRXD...")
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

		kb := model.KeyFrom(req.Market, req.Symbol, "xdxr")
		key := model.NewPrefixKeyFrom(model.TableStock, []byte(kb)).String()
		dividends[key] = append(dividends[key], req)
	}
}

// 获取 XRXD 除权除息
func (s *ApiServer) GetXRXD(ctx context.Context, req *pb.StockRequest) (*pb.XRXDResponse, error) {
	logger.Debugf("GetXRXD of <%s,%s>", req.Market, req.Symbol)
	kb := model.KeyFrom(req.Market, req.Symbol, "xdxr")
	key := model.NewPrefixKeyFrom(model.TableStock, []byte(kb))

	// 存储除权除息信息
	// []*xrxd gob encoding
	var xrxds *pb.XRXDResponse
	rawdata, err := s.rdb.Get(key)
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(rawdata)
	dec := gob.NewDecoder(buf)
	err = dec.Decode(&xrxds)
	if err != nil {
		return nil, err
	}
	return xrxds, nil
}

// 获取 KLine bars 高开低收数据
func (s *ApiServer) GetKLines(ctx context.Context, req *pb.StockRequest) (*pb.KLineResponse, error) {
	logger.Debugf("GetKLines of <%s,%s,%d>", req.Market, req.Symbol, req.Bars)
	uuid1 := model.UniqueKeyFrom(req.Market, req.Symbol)
	prefix := model.NewPrefixKeyFrom(model.TableKLine, uuid1.Bytes())
	// fmt.Printf("scan key, %s\n", prefix.String())

	if req.Bars <= 0 || req.Bars > 3650 {
		req.Bars = 1
	}

	var klines []*pb.KLine
	_, err := s.rdb.ForwardScan(prefix, func(i int, k, v []byte) error {
		kline := &pb.KLine{}
		if err := proto.Unmarshal(v, kline); err != nil {
			return err
		}
		klines = append(klines, kline)
		if i >= int(req.Bars)-1 {
			return &store.Error{Msg: "ok", Code: store.StopIteration} // stop scan
		}
		return nil
	})
	resp := &pb.KLineResponse{KLines: klines}
	return resp, err
}
