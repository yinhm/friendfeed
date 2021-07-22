/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
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
