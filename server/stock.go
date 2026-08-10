package server

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"context"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func archiveStockStream[T any](
	s *ApiServer,
	name string,
	recv func() (T, error),
	archive func(T) error,
	sendAndClose func(*pb.ArchiveSummary) error,
) error {
	slog.Debug("Starting archive stream", "name", name)
	var count int32
	startTime := time.Now()

	// Streaming large imports with Pebble sync writes is prohibitively slow.
	s.rdb.SetSync(false)
	defer s.rdb.SetSync(true)

	for {
		req, err := recv()
		if err == io.EOF {
			return sendAndClose(&pb.ArchiveSummary{
				Count:       count,
				ElapsedTime: int32(time.Since(startTime).Seconds()),
			})
		}
		if err != nil {
			slog.Debug("archive stream error", "name", name, "err", err)
			return err
		}
		count++

		if err := archive(req); err != nil {
			slog.Debug("archive stream error", "name", name, "err", err)
			return err
		}
	}
}

func (s *ApiServer) ArchiveKLine(stream pb.Api_ArchiveKLineServer) error {
	var dateStart int32
	var dateEnd int32
	return archiveStockStream(s, "ArchiveKLine", stream.Recv, func(kReq *pb.KLineRequest) error {
		// Stock KLine:
		// ------------------------------------------------
		// K-> | TableKLine |   uuid   | reverse flake id |
		// ------------------------------------------------
		// V-> |                   <data>                 |
		// ------------------------------------------------
		oldtime := time.Unix(int64(kReq.KLine.Date), 0)
		flakeid := s.rdb.TimeTravelReverseId(oldtime)
		symbolUUID := model.UniqueKeyFrom(kReq.Symbol)
		k := model.NewKeyFrom(symbolUUID.Bytes(), flakeid[:])
		_, err := model.KLine.Put(s.rdb, k, kReq.KLine)
		if err != nil {
			return err
		}

		if dateStart == 0 {
			dateStart = kReq.KLine.Date
		}
		dateEnd = kReq.KLine.Date
		return nil
	}, func(summary *pb.ArchiveSummary) error {
		summary.DateStart = dateStart
		summary.DateEnd = dateEnd
		return stream.SendAndClose(summary)
	})
}

// XRXD 除权除息同步为全量更新
func (s *ApiServer) ArchiveXRXD(stream pb.Api_ArchiveXRXDServer) error {
	slog.Debug("Starting ArchiveXRXD...")
	var count int32
	startTime := time.Now()

	// disable pebble.Sync since we cannot do batch easily
	// and sync was too slow for large data.
	// disable it so we can do 100K/1s records via stream.
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

			return stream.SendAndClose(&pb.ArchiveSummary{
				Count:       count,
				ElapsedTime: int32(time.Since(startTime).Seconds()),
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
	return archiveStockStream(s, "ArchiveRawdata", stream.Recv, func(req *pb.Rawdata) error {
		key := model.KeyFromString(req.Symbol, req.DataType)
		_, err := model.Stock.Put(s.rdb, key, req)
		return err
	}, stream.SendAndClose)
}

// 归档财务数据
func (s *ApiServer) ArchiveFundamental(stream pb.Api_ArchiveFundamentalServer) error {
	return archiveStockStream(s, "ArchiveFundamental", stream.Recv, func(req *pb.Fundamental) error {
		fundamentalUUID := model.UniqueKeyFrom(req.Symbol, "Fundamental")
		_, err := model.Stock.Put(s.rdb, fundamentalUUID.Bytes(), req)
		return err
	}, stream.SendAndClose)
}

// 归档股票基本信息
func (s *ApiServer) ArchiveStockInfo(stream pb.Api_ArchiveStockInfoServer) error {
	return archiveStockStream(s, "ArchiveStockInfo", stream.Recv, func(req *pb.StockInfo) error {
		key := model.KeyFromString(req.Symbol, "StockInfo")
		_, err := model.Stock.Put(s.rdb, key, req)
		return err
	}, stream.SendAndClose)
}

// 获取证券代码列表
func (s *ApiServer) UpdateStockList(ctx context.Context, req *pb.StockList) (*pb.Response, error) {
	slog.Debug("UpdateStockList", "count", len(req.Stocks))

	s.rdb.SetSync(false)
	defer s.rdb.SetSync(true)

	stockListUUID := model.UniqueKeyFrom("stock", "list")
	key := model.NewPrefixKeyFrom(model.TableStock, stockListUUID.Bytes())

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

		slog.Debug("同步股票Feedinfo", "symbol", stock.Symbol)
	}

	return &pb.Response{IsSuccess: true}, nil
}

// 获取证券代码列表
func (s *ApiServer) GetStockList(ctx context.Context, req *pb.StockRequest) (*pb.StockList, error) {
	slog.Debug("GetStockList", "market", req.Market, "match", req.Match)
	stockListUUID := model.UniqueKeyFrom("stock", "list")
	key := model.NewPrefixKeyFrom(model.TableStock, stockListUUID.Bytes())

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
			for item := range strings.SplitSeq(req.Match, ",") {
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
	slog.Debug("GetStock", "symbol", req.Symbol)
	stockListUUID := model.UniqueKeyFrom("stock", "list")
	key := model.NewPrefixKeyFrom(model.TableStock, stockListUUID.Bytes())

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
	slog.Debug("GetStockInfo", "symbol", req.Symbol)

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
	slog.Debug("GetXRXD", "symbol", req.Symbol)
	kb := model.KeyFromString(req.Symbol, "xdxr")
	key := model.NewPrefixKeyFrom(model.TableStock, kb)

	// 存储除权除息信息
	// []*xrxd gob encoding
	var xrxds []*pb.XRXD

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
	slog.Debug("GetKLines", "symbol", req.Symbol, "bars", req.Bars)
	symbolUUID := model.UniqueKeyFrom(req.Symbol)
	prefix := model.NewPrefixKeyFrom(model.TableKLine, symbolUUID.Bytes())
	// fmt.Printf("scan key, %s\n", prefix.String())

	if req.Bars <= 0 {
		req.Bars = 1
	} else if req.Bars > 3650 {
		req.Bars = 3650
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
	slog.Debug("GetFundamental", "symbol", req.Symbol)

	fundamentalUUID := model.UniqueKeyFrom(req.Symbol, "Fundamental")
	msg := new(pb.Fundamental)
	err := model.Stock.Get(s.rdb, fundamentalUUID.Bytes(), msg)

	if err != nil {
		return nil, err
	}

	return msg, nil
}

// 获取证券 req.DataType 获取 Rawdata
func (s *ApiServer) GetRawdata(ctx context.Context, req *pb.StockRequest) (*pb.Rawdata, error) {
	slog.Debug("GetRawdata", "symbol", req.Symbol, "data_type", req.DataType)

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
	slog.Debug("UpdateRawdata", "symbol", req.Symbol, "data_type", req.DataType)

	key := model.KeyFromString(req.Symbol, req.DataType)
	_, err := model.Stock.Put(s.rdb, key, req)

	if err != nil {
		return nil, err
	}

	return &pb.Response{IsSuccess: true}, nil
}

// 删除 Rawdata
func (s *ApiServer) DeleteRawdata(ctx context.Context, req *pb.Rawdata) (*pb.Response, error) {
	slog.Debug("DeleteRawdata", "symbol", req.Symbol, "data_type", req.DataType)

	key := model.KeyFromString(req.Symbol, req.DataType)
	err := model.Stock.Delete(s.rdb, key)

	if err != nil {
		return nil, err
	}

	return &pb.Response{IsSuccess: true}, nil
}

// 发布报告到 Symbol 对应 Feed
func (s *ApiServer) SendReport(ctx context.Context, req *pb.Report) (*pb.Response, error) {
	slog.Debug("SendReport", "symbol", req.Symbol, "type", req.Type)

	profile, err := model.GetProfileFromUserId(s.rdb, req.Symbol)
	if err != nil {
		return nil, err
	}

	dt := time.Now().UTC()
	name := fmt.Sprintf("%x", profile.Uuid) + "/" + dt.Format(time.RFC3339)
	entryUUID := uuid.NewV5(uuid.NamespaceURL, name)

	entry := &pb.Entry{
		Id:          fmt.Sprintf("%x", entryUUID),
		Title:       req.Title,
		RawBody:     req.Body,
		Body:        req.Body,
		Date:        dt.Format(time.RFC3339),
		FeedUuid:    profile.Uuid,
		ProfileUuid: profile.Uuid,
		Type:        "tabular",
	}

	// from is a must
	from := &pb.Feed{
		Id:      profile.Id,
		Name:    profile.Name,
		Type:    profile.Type,
		Picture: profile.Picture,
	}
	entry.From = from

	_, err = s.PostEntry(ctx, entry)
	return &pb.Response{IsSuccess: true}, err
}
