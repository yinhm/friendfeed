package cmd

import (
	"fmt"
	"log"
	"time"

	"github.com/go-gota/gota/dataframe"
	"github.com/go-gota/gota/series"
	"github.com/spf13/cobra"
	"github.com/yinhm/ctdx"
	"github.com/yinhm/ctdx/comm"
	"github.com/yinhm/ctdx/gcom/utils"
	"github.com/yinhm/friendfeed/model"
	pb "github.com/yinhm/friendfeed/proto"
	"golang.org/x/net/context"
)

var tdxConfig string
var stockCode string
var isKLine bool
var isXRXD bool

var stockCmd = &cobra.Command{
	Use:   "stock",
	Short: "sync stock data",
	Long: `sync stock data

	Sync all klines
	cli stock --k
	
	Sync stock feed, default sync all
	cli stock --tdxcfg /home/yinhm/tdx/config.toml --n 600519
    `,
	Run: func(cmd *cobra.Command, args []string) {
		if tdxConfig == "" {
			fmt.Println("No config file.")
			return
		}

		switch {
		case isKLine:
			if err := syncKline(); err != nil {
				log.Println(err)
			}
			return
		case isXRXD:
			syncXRXD()
		default:
			err := syncProfile()
			if err != nil {
				log.Println(err)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(stockCmd)
	stockCmd.Flags().StringVar(&stockCode, "n", "", "stock code")
	stockCmd.Flags().BoolVar(&isKLine, "kline", false, "sync daily kline")
	stockCmd.Flags().BoolVar(&isXRXD, "xrxd", false, "stock dividend")
	stockCmd.Flags().StringVar(&tdxConfig, "tdxconfig", "config.toml", "tdx config file")
}

func marketCodeToString(market int) string {
	name := "SH"
	if market == 0 {
		name = "SZ"
	}
	return name
}

// 为每只证券创建其对应 Feed
func syncProfile() error {
	cfg := new(comm.Conf)
	cfg.Parse(tdxConfig)

	// 默认加载股票交易日历数据
	calendarPath := fmt.Sprintf("%s%s", cfg.App.DataPath, cfg.Tdx.Files.Calendar)
	_, err := comm.DefaultStockCalendar(calendarPath)

	if nil != err {
		log.Printf("%v", err)
		return err
	}

	log.Println("更新基础的股票数据...")
	api := ctdx.NewDefaultTdxClient(cfg)
	defer api.Close()

	// 股指基
	df := comm.GetFinanceDataFrame(api.Configure, comm.STOCKA, comm.STOCKB, comm.INDEX, comm.FUNDS)
	if nil != df.Err {
		log.Printf("读取股票基础数据失败! err:%v", df)
		return err
	}
	log.Printf("股票基础数据: %v", df)

	for _, row := range df.Maps() {
		var code [6]byte
		market := row["market"].(int)
		strCode := row["code"].(string)
		copy(code[:], []byte(strCode))
		name := row["name"].(string)

		mktName := ""
		if market == 0 {
			mktName = "SZ"
		} else if market == 1 {
			mktName = "SH"
		}

		if stockCode != "" && strCode != stockCode {
			continue
		}

		feedinfo := &pb.Feedinfo{
			Uuid:        model.UniqueKeyFrom(mktName, strCode).String(),
			Id:          strCode,
			Name:        name,
			Type:        "sys",
			Private:     false,
			Description: fmt.Sprintf("<%s, %s>", mktName, strCode),
		}
		_, err := agent.client.PostFeedinfo(context.Background(), feedinfo)
		if err != nil {
			return err
		}

		log.Printf("同步股票Feedinfo: %s %s", mktName, strCode)
	}
	return nil
}

// 更新股票日线数据
func syncKline() error {
	cfg := new(comm.Conf)
	cfg.Parse(tdxConfig)

	// 默认加载股票交易日历数据
	calendarPath := fmt.Sprintf("%s%s", cfg.App.DataPath, cfg.Tdx.Files.Calendar)
	_, err := comm.DefaultStockCalendar(calendarPath)

	if nil != err {
		log.Printf("%v", err)
		return err
	}

	api := ctdx.NewDefaultTdxClient(cfg)
	defer api.Close()

	// 股指基
	df := comm.GetFinanceDataFrame(api.Configure, comm.STOCKA, comm.STOCKB, comm.INDEX, comm.FUNDS)
	if nil != df.Err {
		log.Printf(fmt.Sprintf("读取股票基础数据失败! err:%v", df))
		return df.Err
	}

	stream, err := agent.client.ArchiveKLine(context.Background())
	defer stream.CloseAndRecv()
	if err != nil {
		return err
	}

	for _, row := range df.Maps() {
		var code [6]byte
		market := row["market"].(int)
		strCode := row["code"].(string)

		copy(code[:], []byte(strCode))
		mktName := "SH"
		if market == 0 {
			mktName = "SZ"
		}
		log.Printf("同步 %s:%s 的日线数据...", mktName, strCode)

		fileName := fmt.Sprintf("%d%s.csv", market, strCode)

		stocksPath := fmt.Sprintf("%s%s%s", api.Configure.GetApp().DataPath,
			api.Configure.GetTdx().Files.StockDay, fileName)

		colTypes := map[string]series.Type{
			"market": series.Int, "code": series.String, "date": series.String, "open": series.Float, "low": series.Float,
			"high": series.Float, "close": series.Float, "volume": series.Int, "amount": series.Float}

		ohlcs := utils.ReadCSV(stocksPath, dataframe.WithTypes(colTypes))
		for _, k := range ohlcs.Maps() {
			date := k["date"].(string)
			// parse time from "20210607"
			dateFull := fmt.Sprintf("%s-%s-%sT00:00:00+08:00", date[:4], date[4:6], date[6:8])
			dt, err := time.Parse(time.RFC3339, dateFull)
			if err != nil {
				log.Printf("Can not parse time %s", err)
			}
			kp := &pb.KLineRequest{
				Symbol: strCode,
				Market: mktName,
				KLine: &pb.KLine{
					Date:   int32(dt.Unix()),
					Open:   float32(k["open"].(float64)),
					High:   float32(k["high"].(float64)),
					Low:    float32(k["low"].(float64)),
					Close:  float32(k["close"].(float64)),
					Volume: int64(k["volume"].(int)),
					Amount: float32(k["amount"].(float64)),
				},
			}

			if err := stream.Send(kp); err != nil {
				log.Printf("%v.Send(%v) = %v", stream, kp, err)
				return err
			}
		}
	}
	return nil
}

// 更新股票高送转数据
//
// 高送转文件格式解析

// 股票代码(code), 日期(date), 所属市场(market), 高送转类型(type), 送现金(money), 配股价(price), 送股数(count),  配股比例(rate)

// ```
// code, date, market, type, money,  price,  count,  rate
//               1,6,13,14  分红,    配股价,  转送股,  配股      # 复权只计算 1.除权除息
//  2,3,4,5,7,8,9,10,11,12  前流通盘, 前总股本, 后流通盘, 后总股本
// ```

// * type的取值与含义:
// > 1=除权除息 <br/>
// > 2=送配股上市(如: 000656 2015-04-29) <br/>
// > 3=非流通股上市(如: 000656 2010-02-10) <br/>
// > 4=未知股本变动(如: 600642 1993-07-19) <br/>
// > 5=股本变化(如: 000656 2017-06-30) <br/>
// > 6=增发新股(如: 600887 2002-08-20) <br/>
// > 7=股份回购(如: 600619 2000-09-08) <br/>
// > 8=增发新股上市(如: 600186 2001-02-14) <br/>
// > 9=转配股上市(如: 600811 2017-07-25) <br/>
// > 10=可转债上市(如: 600418 2006-07-07) <br/>
// > 11=扩缩股(如: 600381 2014-06-27) <br/>
// > 12=非流通股缩股(如: 600339 2006-04-10) <br/>
// > 13=送认购权证(如: 600008 2006-04-19) <br/>
// > 14=送认沽权证(如: 000932 2006-03-01)

// 根据type取值的不同，`money`、`price`、`count`、`rate` 的含义也不同:
// * 在除权除息(type=1)或者增发新股(type=6)时，含义分别是: `分红(money), 配股价(price), 送股数(count),  配股比例(rate)`
// * 其它取值时，含义分别是: `前流通盘(money), 前总股本(price), 后流通盘(count),  后总股本(rate)`

// 我们暂时只考虑type=1及除权除息数据
func syncXRXD() error {
	cfg := new(comm.Conf)
	cfg.Parse(tdxConfig)

	// 默认加载股票交易日历数据
	calendarPath := fmt.Sprintf("%s%s", cfg.App.DataPath, cfg.Tdx.Files.Calendar)
	_, err := comm.DefaultStockCalendar(calendarPath)

	if nil != err {
		log.Printf("%v", err)
		return err
	}

	api := ctdx.NewDefaultTdxClient(cfg)
	defer api.Close()

	stream, err := agent.client.ArchiveXRXD(context.Background())
	if err != nil {
		return err
	}

	dividendPath := fmt.Sprintf("%s%s", cfg.App.DataPath, cfg.Tdx.Files.StockBonus)
	dtypes := map[string]series.Type{
		"code": series.String, "date": series.String,
		"market": series.Int, "type": series.Int,
		"money": series.Float, "price": series.Float,
		"count": series.Float, "rate": series.Float,
	}

	df := utils.ReadCSV(dividendPath, dataframe.WithTypes(dtypes))
	for _, row := range df.Maps() {
		// 单取除权除息数据
		if row["type"] != 1 {
			continue
		}

		date := row["date"].(string)
		// parse time from "20210607"
		dateFull := fmt.Sprintf("%s-%s-%sT00:00:00+08:00", date[:4], date[4:6], date[6:8])
		dt, err := time.Parse(time.RFC3339, dateFull)
		if err != nil {
			log.Printf("Can not parse time %s", err)
		}
		msg := &pb.XRXD{
			Symbol:        row["code"].(string),
			Market:        marketCodeToString(row["market"].(int)),
			ExDate:        int32(dt.Unix()), // ex date?
			Dividend:      float32(row["money"].(float64)),
			PurchasePrice: float32(row["price"].(float64)),
			Split:         float32(row["count"].(float64)),
			Purchase:      float32(row["rate"].(float64)),
		}

		if err := stream.Send(msg); err != nil {
			log.Printf("%v.Send(%v) = %v", stream, msg, err)
			return err
		}
	}

	summary, err := stream.CloseAndRecv()
	log.Printf("xrxd count: %d", summary.Count)

	return nil
}
