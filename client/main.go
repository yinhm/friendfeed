// copyright 2015 The Lastff Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// debug archived
// go run main.go -u=foobar -d=true
//
// Mark deletion
// go run main.go --cmd="MarkDelete" --arg1="foobar"
package main

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"time"

	ttext "github.com/cupcake/text-entities-go"
	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	"github.com/gofrs/uuid"
	pb "github.com/yinhm/friendfeed/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

var config struct {
	address  string
	username string
	file     string
	command  string
	arg1     string
	debug    bool
}

type TwitterConfig struct {
	ApiKey    string `json:"twitter_api_key"`
	ApiSecret string `json:"twitter_api_secret"`
}

func init() {
	flag.StringVar(&config.address, "addr", "localhost:8901", "RPC Server Url")
	flag.StringVar(&config.file, "c", "/srv/ffdb/config.json", "config file")
	flag.StringVar(&config.command, "cmd", "", "cmd execution")
	flag.StringVar(&config.arg1, "arg1", "", "pass argument to command")
	flag.StringVar(&config.username, "u", "", "debug user feed")
	flag.BoolVar(&config.debug, "d", false, "Enable debug info.")
}

func NewConfigFromJSON(filename string) (*TwitterConfig, error) {
	rawdata, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}

	config := new(TwitterConfig)
	if err := json.Unmarshal(rawdata, &config); err != nil {
		return nil, err
	}
	return config, nil
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

func NewFeedAgent(conn *grpc.ClientConn) *FeedAgent {
	c := pb.NewApiClient(conn)
	worker := &pb.Worker{
		Id: randhash(),
	}
	return &FeedAgent{
		client: c,
		worker: worker,
	}
}

func (fa *FeedAgent) Start() {
	if config.command != "" {
		cmd := &pb.CommandRequest{
			Command: config.command,
		}
		if config.arg1 != "" {
			cmd.Arg1 = config.arg1
		}
		fa.client.Command(context.Background(), cmd)
		return
	}

	if config.debug && config.username != "" {
		if err := fa.Debug(config.username); err != nil {
			log.Fatalf("Debug failed: %s", err)
		}
		return
	}

	log.Print("start processing...")

	// lazy init
	tc, err := NewConfigFromJSON(config.file)
	if err != nil {
		log.Fatal(err)
	}
	fa.tcCfg = tc

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
	// OAuth1 http.Client will automatically authorize Requests
	httpClient := config.Client(oauth1.NoContext, token)
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

func main() {
	flag.Parse()

	conn, err := grpc.Dial(config.address, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Connection error: %v", err)
	}
	defer conn.Close()

	agent := NewFeedAgent(conn)
	agent.Start()
}
