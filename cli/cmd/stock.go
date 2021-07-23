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
var isKline bool

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
		if isKline {
			if err := syncKline(); err != nil {
				log.Println(err)
			}
			return
		}
		err := sync()
		if err != nil {
			log.Println(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(stockCmd)
	stockCmd.Flags().StringVar(&stockCode, "n", "", "stock code")
	stockCmd.Flags().BoolVar(&isKline, "k", false, "sync daily kline")
	stockCmd.Flags().StringVar(&tdxConfig, "tdxconfig", "config.toml", "tdx config file")
}

func sync() error {
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
