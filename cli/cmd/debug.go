package cmd

import (
	"context"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/pb"
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
		if err := debugFeed(debugFeedName); err != nil {
			log.Fatalf("Debug failed: %s", err)
		}
	},
}

func debugFeed(name string) error {
	feed, err := apiClient.FetchFeed(context.Background(), &pb.FeedRequest{
		Id: name, PageSize: 50,
	})
	if err != nil {
		return err
	}
	log.Printf("feed: %v", feed.Id)
	log.Printf("feed.Entries: %d", len(feed.Entries))
	for _, entry := range feed.Entries {
		log.Println(entry.Id, entry.Date, entry.RawBody)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(debugCmd)
	debugCmd.Flags().StringVar(&debugFeedName, "u", "", "username/feedname")
}
