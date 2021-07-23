package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

var debugFeedName string

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "debug user feed",
	Long: `debug archived
	client debug ---u foobar
    `,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("debug feed: %s\n", debugFeedName)
		if debugFeedName == "" {
			return
		}
		if err := agent.Debug(debugFeedName); err != nil {
			log.Fatalf("Debug failed: %s", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(debugCmd)
	debugCmd.Flags().StringVar(&debugFeedName, "u", "", "username/feedname")
}
