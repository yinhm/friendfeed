package cmd

import (
	"fmt"
	"log"
	"strings"

	"github.com/gofrs/uuid"
	"github.com/spf13/cobra"
	"github.com/yinhm/ctdx"
	"github.com/yinhm/ctdx/comm"
	pb "github.com/yinhm/friendfeed/proto"
	"golang.org/x/net/context"
)

var tdxConfig string
var stockName string

var stockCmd = &cobra.Command{
	Use:   "stock",
	Short: "sync stock data",
	Long: `sync stock data

	client stock --tdxcfg /home/yinhm/tdx/config.toml --n 600519
    `,
	Run: func(cmd *cobra.Command, args []string) {
		if tdxConfig == "" {
			fmt.Printf("sync stock: %s\n", stockName)
			return
		}
		fmt.Printf("sync stock: %s\n", stockName)
		if stockName == "" {
			return
		}
		err := sync(agent)
		if err != nil {
			log.Println(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(stockCmd)
	stockCmd.Flags().StringVar(&stockName, "n", "600519", "stockName")
	stockCmd.Flags().StringVar(&tdxConfig, "tdxconfig", "config.toml", "tdx config file")
}

func sync(agent *FeedAgent) error {
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

		uniqueName := fmt.Sprintf("%s:%s", mktName, strCode)
		uuid := uuid.NewV5(uuid.NamespaceURL, strings.ToLower(uniqueName))

		feedinfo := &pb.Feedinfo{
			Uuid:        uuid.String(),
			Id:          strCode,
			Name:        name,
			Type:        "sys",
			Private:     false,
			Description: uniqueName,
		}
		_, err := agent.client.PostFeedinfo(context.Background(), feedinfo)
		if err != nil {
			return err
		}

		log.Printf("同步股票Feedinfo: %s", uniqueName)
		break
	}
	return nil
}
