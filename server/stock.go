package server

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
		uuid1 := model.UniqueKeyFrom(kReq.Symbol)
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
				k := store.KeyFromString(k)
				err = s.rdb.Put(k, buf.Bytes())
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

		kb := model.KeyFromString(req.Symbol, "xdxr")
		key := model.NewPrefixKeyFrom(model.TableStock, kb).String()
		dividends[key] = append(dividends[key], req)
	}
}

// 下列接口给 python 客户端使用
// --------------------------
// 根据 Rawdata.DataType 归档股票数据
func (s *ApiServer) ArchiveRawdata(stream pb.Api_ArchiveRawdataServer) error {
	logger.Debugln("Starting ArchiveRawdata...")
	var count int32
	startTime := time.Now()

	// disable pebble.Sync since we cannot do batch easily
	// and sync was too slow for large data.
	s.rdb.SetSync(false)
	defer s.rdb.SetSync(true)

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			endTime := time.Now()
			return stream.SendAndClose(&pb.ArchiveSummary{
				Count:       count,
				ElapsedTime: int32(endTime.Sub(startTime).Seconds()),
			})
		}
		if err != nil {
			logger.Debugf("ArchiveRawdata error: %v", err)
			return err
		}
		count++

		key := model.KeyFromString(req.Symbol, req.DataType)
		_, err = model.Stock.Put(s.rdb, key, req)
		if err != nil {
			logger.Debugf("ArchiveRawdata error: %v", err)
			return err
		}
	}
}

// 归档财务数据
func (s *ApiServer) ArchiveFundamental(stream pb.Api_ArchiveFundamentalServer) error {
	logger.Debugln("Starting ArchiveFundamental...")
	var count int32
	startTime := time.Now()

	// disable pebble.Sync since we cannot do batch easily
	// and sync was too slow for large data.
	s.rdb.SetSync(false)
	defer s.rdb.SetSync(true)

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			endTime := time.Now()
			return stream.SendAndClose(&pb.ArchiveSummary{
				Count:       count,
				ElapsedTime: int32(endTime.Sub(startTime).Seconds()),
			})
		}
		if err != nil {
			logger.Debugf("ArchiveFundamental error: %v", err)
			return err
		}
		count++

		uuid1 := model.UniqueKeyFrom(req.Symbol, "Fundamental")
		_, err = model.Stock.Put(s.rdb, uuid1.Bytes(), req)
		if err != nil {
			logger.Debugf("ArchiveFundamental error: %v", err)
			return err
		}
	}
}

// 归档股票基本信息
func (s *ApiServer) ArchiveStockInfo(stream pb.Api_ArchiveStockInfoServer) error {
	logger.Debugln("Starting ArchiveStockInfo...")
	var count int32
	startTime := time.Now()

	// disable pebble.Sync since we cannot do batch easily
	// and sync was too slow for large data.
	s.rdb.SetSync(false)
	defer s.rdb.SetSync(true)

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			endTime := time.Now()
			return stream.SendAndClose(&pb.ArchiveSummary{
				Count:       count,
				ElapsedTime: int32(endTime.Sub(startTime).Seconds()),
			})
		}
		if err != nil {
			logger.Debugf("ArchiveStockInfo error: %v", err)
			return err
		}
		count++

		key := model.KeyFromString(req.Symbol, "StockInfo")
		_, err = model.Stock.Put(s.rdb, key, req)
		if err != nil {
			logger.Debugf("ArchiveStockInfo error: %v", err)
			return err
		}
	}
}

// 获取证券代码列表
func (s *ApiServer) UpdateStockList(ctx context.Context, req *pb.StockList) (*pb.Response, error) {
	logger.Debugf("UpdateStockList, count %d", len(req.Stocks))

	s.rdb.SetSync(false)
	defer s.rdb.SetSync(true)

	uuid1 := model.UniqueKeyFrom("stock", "list")
	key := model.NewPrefixKeyFrom(model.TableStock, uuid1.Bytes())

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(req.Stocks)
	if err != nil {
		return nil, err
	}
	err = s.rdb.Put([]byte(key), buf.Bytes())
	if err != nil {
		return nil, err
	}

	for _, stock := range req.Stocks {
		feedinfo := &pb.Feedinfo{
			Uuid:        model.UniqueKeyFrom(stock.Symbol).String(),
			Id:          stock.Symbol,
			Name:        stock.Name,
			Type:        "group",
			Private:     false,
			Description: fmt.Sprintf("<%s>", stock.Symbol),
		}
		_, err := s.PostFeedinfo(context.Background(), feedinfo)
		if err != nil {
			return nil, err
		}

		logger.Debugf("同步股票Feedinfo: %s", stock.Symbol)
	}

	return &pb.Response{IsSuccess: true}, nil
}

// 获取证券代码列表
func (s *ApiServer) GetStockList(ctx context.Context, req *pb.StockRequest) (*pb.StockList, error) {
	logger.Debugf("GetStockList of <%s,%s>", req.Market, req.Match)
	uuid1 := model.UniqueKeyFrom("stock", "list")
	key := model.NewPrefixKeyFrom(model.TableStock, uuid1.Bytes())

	resp := &pb.StockList{}

	var stocks []*pb.Stock
	rawdata, err := s.rdb.Get(key)
	if err != nil || len(rawdata) == 0 {
		return resp, err
	}
	buf := bytes.NewBuffer(rawdata)
	dec := gob.NewDecoder(buf)
	err = dec.Decode(&stocks)
	if err != nil {
		return resp, err
	}

	for _, stock := range stocks {
		if req.Market != "" && !strings.HasSuffix(stock.Symbol, req.Market) {
			continue
		}

		if req.Match != "" {
			matched := false
			for _, item := range strings.Split(req.Match, ",") {
				if strings.HasPrefix(stock.Symbol, item) {
					matched = true
				}
			}
			if !matched {
				continue
			}
		}
		resp.Stocks = append(resp.Stocks, stock)
	}

	return resp, nil
}

// 获取证券名称
// pb.Stock 包含最基本证券信息，进一步信息参见 StockInfo
// TODO: optimise
func (s *ApiServer) GetStock(ctx context.Context, req *pb.StockRequest) (*pb.Stock, error) {
	logger.Debugf("GetStock of <%s>", req.Symbol)
	uuid1 := model.UniqueKeyFrom("stock", "list")
	key := model.NewPrefixKeyFrom(model.TableStock, uuid1.Bytes())

	var stocks []*pb.Stock
	rawdata, err := s.rdb.Get(key)
	if err != nil || len(rawdata) == 0 {
		return nil, err
	}
	buf := bytes.NewBuffer(rawdata)
	dec := gob.NewDecoder(buf)
	err = dec.Decode(&stocks)
	if err != nil {
		return nil, err
	}
	for _, stock := range stocks {
		if req.Symbol == stock.Symbol {
			return stock, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "not found")
}

// 获取证券基本信息
// 证券所属行业等
func (s *ApiServer) GetStockInfo(ctx context.Context, req *pb.StockRequest) (*pb.StockInfo, error) {
	logger.Debugf("GetStock of <%s>", req.Symbol)

	key := model.KeyFromString(req.Symbol, "StockInfo")
	msg := new(pb.StockInfo)
	err := model.Stock.Get(s.rdb, key, msg)

	if err != nil {
		return nil, err
	}

	return msg, nil
}

// 获取 XRXD 除权除息
func (s *ApiServer) GetXRXD(ctx context.Context, req *pb.StockRequest) (*pb.XRXDResponse, error) {
	logger.Debugf("GetXRXD of <%s>", req.Symbol)
	kb := model.KeyFromString(req.Symbol, "xdxr")
	key := model.NewPrefixKeyFrom(model.TableStock, kb)

	// 存储除权除息信息
	// []*xrxd gob encoding
	var xrxds []*pb.XRXD

	fmt.Println("get key: ", hex.EncodeToString(key))
	rawdata, err := s.rdb.Get(key)
	if err != nil {
		return nil, err
	}
	// allow empty xrxd such as index, fund
	if len(rawdata) == 0 {
		return &pb.XRXDResponse{}, nil
	}
	buf := bytes.NewBuffer(rawdata)
	dec := gob.NewDecoder(buf)
	err = dec.Decode(&xrxds)
	if err != nil {
		return nil, err
	}

	resp := &pb.XRXDResponse{
		XRXDS: xrxds,
	}
	return resp, nil
}

// 获取 KLine bars 高开低收数据
func (s *ApiServer) GetKLines(ctx context.Context, req *pb.StockRequest) (*pb.KLineResponse, error) {
	logger.Debugf("GetKLines of <%s, %d>", req.Symbol, req.Bars)
	uuid1 := model.UniqueKeyFrom(req.Symbol)
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

// 获取证券财务信息
func (s *ApiServer) GetFundamental(ctx context.Context, req *pb.StockRequest) (*pb.Fundamental, error) {
	logger.Debugf("GetFundamental of <%s>", req.Symbol)

	uuid1 := model.UniqueKeyFrom(req.Symbol, "Fundamental")
	msg := new(pb.Fundamental)
	err := model.Stock.Get(s.rdb, uuid1.Bytes(), msg)

	if err != nil {
		return nil, err
	}

	return msg, nil
}

// 获取证券 req.DataType 获取 Rawdata
func (s *ApiServer) GetRawdata(ctx context.Context, req *pb.StockRequest) (*pb.Rawdata, error) {
	logger.Debugf("GetRawdata of <%s, %s>", req.Symbol, req.DataType)

	key := model.KeyFromString(req.Symbol, req.DataType)
	msg := new(pb.Rawdata)
	err := model.Stock.Get(s.rdb, key, msg)

	if err != nil {
		return nil, err
	}

	return msg, nil
}

// 更新 Rawdata
func (s *ApiServer) UpdateRawdata(ctx context.Context, req *pb.Rawdata) (*pb.Response, error) {
	logger.Debugf("UpdateRawdata of <%s, %s>", req.Symbol, req.DataType)

	key := model.KeyFromString(req.Symbol, req.DataType)
	_, err := model.Stock.Put(s.rdb, key, req)

	if err != nil {
		return nil, err
	}

	return &pb.Response{IsSuccess: true}, nil
}

// 删除 Rawdata
func (s *ApiServer) DeleteRawdata(ctx context.Context, req *pb.Rawdata) (*pb.Response, error) {
	logger.Debugf("DeleteRawdata of <%s, %s>", req.Symbol, req.DataType)

	key := model.KeyFromString(req.Symbol, req.DataType)
	err := model.Stock.Delete(s.rdb, key)

	if err != nil {
		return nil, err
	}

	return &pb.Response{IsSuccess: true}, nil
}
