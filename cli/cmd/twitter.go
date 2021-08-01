// copyright 2015 The Lastff Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package cmd

import (
	"fmt"
	"log"
	"strings"
	"time"

	ttext "github.com/cupcake/text-entities-go"
	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	"github.com/gofrs/uuid"
	"github.com/spf13/cobra"
	"github.com/yinhm/friendfeed/pb"
	"golang.org/x/net/context"
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "twitter",
	Short: "同步Twitter数据",
	Long:  `执行服务器Job任务，目前仅为获取Twitter数据`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("sync twitter...")
		agent.Start()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// serveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func (fa *FeedAgent) newJob() (*pb.FeedJob, error) {
	feedjob, err := fa.client.GetFeedJob(context.Background(), fa.worker)
	if err != nil {
		return nil, err
	}
	return feedjob, nil
}

func (fa *FeedAgent) process(job *pb.FeedJob) error {
	log.Printf("Start fetching entries for: %s", job.Id)
	total, err := fa.fetchService(job)
	if err != nil {
		return err
	}

	job, err = fa.client.FinishJob(context.Background(), job)
	if err != nil {
		return err
	}

	log.Printf("Job done for %s, %d entries", job.Id, total)
	return nil
}

func (fa *FeedAgent) fetchService(job *pb.FeedJob) (int, error) {
	stream, err := fa.client.ArchiveFeed(context.Background())
	defer stream.CloseAndRecv()
	if err != nil {
		return 0, err
	}

	updated := time.Unix(job.Service.Updated, 0)
	authinfo := job.Service.Oauth
	if authinfo == nil {
		return 0, fmt.Errorf("skip job: no authinfo")
	}

	config := oauth1.NewConfig(fa.tcCfg.ApiKey, fa.tcCfg.ApiSecret)
	token := oauth1.NewToken(authinfo.AccessToken, authinfo.AccessTokenSecret)

	// timeout ctx does not work?
	ctx, cncl := context.WithTimeout(context.Background(), time.Second*10)
	defer cncl()
	httpClient := config.Client(ctx, token)

	// Twitter client
	api := twitter.NewClient(httpClient)

	// user timeline
	params := &twitter.UserTimelineParams{ScreenName: "yinhm", Count: 10}
	tweets, _, err := api.Timelines.UserTimeline(params)
	if err != nil {
		log.Printf("UserTimeline: %s", err)
		return 0, fmt.Errorf("UserTimeline: %s", err)
	}

	n := 0
	for i := len(tweets) - 1; i >= 0; i-- {
		tweet := tweets[i]

		// skip reply status
		if tweet.InReplyToStatusID != 0 {
			// fmt.Printf("skip reply: %s\n", tweet.IDStr)
			continue
		}

		url := "https://twitter.com/" + tweet.User.ScreenName + "/status/" + tweet.IDStr
		// deterministic uuid otherwise feed will be polluted
		uuid1 := uuid.NewV5(uuid.NamespaceURL, url)
		tt, err := tweet.CreatedAtTime()
		if err != nil || tt.Before(updated) {
			fmt.Printf("skip updated: %s\n", tt)
			continue
		}

		from := &pb.Feed{
			Id:   job.Profile.Id,
			Name: job.Profile.Name,
			Type: job.Profile.Type,
		}

		var thumbnails []*pb.Thumbnail
		if tweet.ExtendedEntities != nil {
			for _, media := range tweet.ExtendedEntities.Media {
				if media.Type != "photo" {
					continue
				}

				url := ""
				if media.MediaURLHttps != "" {
					url = media.MediaURLHttps
				} else {
					url = media.MediaURL
				}
				thumb := &pb.Thumbnail{
					Url:    url,
					Link:   media.ExpandedURL,
					Width:  int32(media.Sizes.Small.Width),
					Height: int32(media.Sizes.Small.Height),
				}
				thumbnails = append(thumbnails, thumb)
			}
		}

		body := tweet.Text
		tags := ttext.ExtractHashtags(body)
		for _, tag := range tags {
			new := fmt.Sprintf("<a href=\"https://twitter.com/hashtag/%s\">%s</a>", tag, tag)
			body = strings.Replace(body, tag, new, -1)
		}
		urls := ttext.ExtractURLs(tweet.Text)
		for _, url := range urls {
			new := fmt.Sprintf("<a href=\"%s\">%s</a>", url, url)
			body = strings.Replace(body, url, new, -1)
		}

		entry := &pb.Entry{
			Id:      uuid1.String(),
			Url:     url,
			Date:    tt.Format(time.RFC3339),
			Body:    body,
			RawBody: tweet.Text,
			RawLink: url,
			From:    from,
			// To:         []*pb.Feed{from},
			Thumbnails: thumbnails,
			Via: &pb.Via{
				Name: "Twitter",
				Url:  url,
			},
			ProfileUuid: job.Profile.Uuid,
		}

		// fmt.Printf("stream.send: %s\n", uuid1.String())

		if err := stream.Send(entry); err != nil {
			log.Printf("%v.Send(%v) = %v", stream, entry, err)
			return n, err
		}

		n++
	}
	return n, nil
}
