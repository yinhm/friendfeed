package cmd

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	pb "github.com/yinhm/friendfeed/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

var config struct {
	address  string
	datapath string
	debug    bool
}

var agent *FeedAgent
var rpcConn *grpc.ClientConn

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "client",
	Short: "ffdb同步数据客户端",
	Long:  `CLI客户端，--help for more information`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// all agents inited here
		tcCfg := &TwitterConfig{
			ApiKey:    viper.GetString("twitter_api_key"),
			ApiSecret: viper.GetString("twitter_api_secret"),
		}

		var err error
		rpcConn, err = grpc.Dial(config.address, grpc.WithInsecure())
		if err != nil {
			log.Fatalf("Connection error: %v", err)
		}
		agent = NewFeedAgent(rpcConn, tcCfg)
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		defer rpcConn.Close()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&config.address, "address", "localhost:8901", "RPC Server address")
	rootCmd.PersistentFlags().StringVar(&config.datapath, "path", "/srv/ffdb/", "data and config path")
	rootCmd.PersistentFlags().BoolVar(&config.debug, "debug", false, "enable debug")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	cfgFile := filepath.Join(config.datapath, "config.json")
	viper.SetConfigFile(cfgFile)

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

type TwitterConfig struct {
	ApiKey    string `json:"twitter_api_key"`
	ApiSecret string `json:"twitter_api_secret"`
}

func randhash() string {
	randbytes := make([]byte, 4)
	rand.Read(randbytes)

	h := sha1.New()
	h.Write(randbytes)
	return hex.EncodeToString(h.Sum(nil))[:12]
}

type FeedAgent struct {
	client pb.ApiClient
	worker *pb.Worker
	tcCfg  *TwitterConfig
}

func NewFeedAgent(conn *grpc.ClientConn, cfg *TwitterConfig) *FeedAgent {
	c := pb.NewApiClient(conn)
	worker := &pb.Worker{
		Id: randhash(),
	}
	return &FeedAgent{
		client: c,
		worker: worker,
		tcCfg:  cfg,
	}
}

func (fa *FeedAgent) Start() {
	log.Print("start processing...")

	// run feed mirror job forever
	for {
		job, err := fa.newJob()
		if err != nil {
			log.Printf("Get job failed: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if err := fa.process(job); err != nil {
			log.Printf("Archive failed: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}
	}
}

func (fa *FeedAgent) Debug(name string) error {
	req := &pb.FeedRequest{
		Id:       name,
		Start:    0,
		PageSize: 50,
	}
	feed, err := fa.client.FetchFeed(context.Background(), req)
	if err != nil {
		return err
	}
	log.Printf("feed: %v", feed.Id)
	log.Printf("feed.Entries: %d", len(feed.Entries))

	for _, e := range feed.Entries {
		log.Println(e.Id, e.Date, e.RawBody)
	}
	return nil
}
