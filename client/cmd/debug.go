package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
)

var debugFeedName string

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "debug user feed",
	Long: `debug archived
	client debug -u=foobar
    `,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("debug feed: %s\n", debugFeedName)
		if debugFeedName == "" {
			return
		}

		tcCfg := &TwitterConfig{
			ApiKey:    viper.GetString("twitter_api_key"),
			ApiSecret: viper.GetString("twitter_api_secret"),
		}

		conn, err := grpc.Dial(config.address, grpc.WithInsecure())
		if err != nil {
			log.Fatalf("Connection error: %v", err)
		}
		defer conn.Close()

		agent := NewFeedAgent(conn, tcCfg)

		if err := agent.Debug(debugFeedName); err != nil {
			log.Fatalf("Debug failed: %s", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(debugCmd)
	debugCmd.Flags().StringVar(&debugFeedName, "u", "", "username/feedname")
}
