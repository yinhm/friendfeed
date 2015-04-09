// Copyright 2015 The Lastff Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// default crawl client
// go run main.go -c=/srv/httpcache
//
// enque job
// go run main.go -u=name -p=pwd
// go run main.go -u=name -p=pwd -t=friendfeed-feedback
//
// debug archived
// go run main.go -u=yinhm -d=true
package main

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/yinhm/friendfeed/ff"
	pb "github.com/yinhm/friendfeed/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

var config struct {
	address  string
	username string
	authkey  string
	target   string
	cacheDir string
	command  string
	debug    bool
}

func init() {
	flag.StringVar(&config.address, "addr", "localhost:8901", "RPC Server Url")
	flag.StringVar(&config.username, "u", "", "Friendfeed username")
	flag.StringVar(&config.authkey, "p", "", "Friendfeed remote key, obtain here: https://friendfeed.com/account/api")
	flag.StringVar(&config.target, "t", "", "Feed to fetch, default to username")
	flag.StringVar(&config.cacheDir, "c", "", "Path of httpcach, api call caching will enable if not empty.")
	flag.BoolVar(&config.debug, "d", false, "Enable debug info.")
	flag.StringVar(&config.command, "cmd", "", "cmd execution")
}

func randhash() string {
	randbytes := make([]byte, 4)
	rand.Read(randbytes)

	h := sha1.New()
	h.Write(randbytes)
	return hex.EncodeToString(h.Sum(nil))[:12]
}

type MirrorAgent struct {
	client pb.ApiClient
	worker *pb.Worker
	apiv1  *ff.Client
	apiv2  *ff.Client

	archived map[string]bool
}

func NewMirrorAgent(conn *grpc.ClientConn) *MirrorAgent {
	c := pb.NewApiClient(conn)
	worker := &pb.Worker{
		Id: randhash(),
	}
	return &MirrorAgent{
		client:   c,
		worker:   worker,
		archived: make(map[string]bool),
	}
}

func (ma *MirrorAgent) Start() {
	if config.command != "" {
		cmd := &pb.CommandRequest{config.command}
		ma.client.Command(context.Background(), cmd)
		return
	}

	if config.username != "" && config.authkey != "" {
		if err := ma.EnqueJob(config.username, config.authkey, config.target); err != nil {
			log.Fatalf("Enque failed: %s", err)
		}
		log.Print("Job enqueued, exit.")
		return
	}

	if config.debug && config.username != "" {
		if err := ma.Debug(config.username); err != nil {
			log.Fatalf("Debug failed: %s", err)
		}
		return
	}

	log.Print("start processing...")
	// run feed mirror job forever
	for {
		if err := ma.newJob(); err != nil {
			log.Printf("Get job failed: %v", err)
			log.Println("Sleep for 5 seconds.")
			time.Sleep(5 * time.Second)
			continue
		}

		if err := ma.archive(); err != nil {
			log.Printf("Archive failed: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}
	}
}

func (ma *MirrorAgent) Debug(name string) error {
	req := &pb.FeedRequest{
		Id:       name,
		Start:    0,
		PageSize: 50,
	}
	feed, err := ma.client.FetchFeed(context.Background(), req)
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

func (ma *MirrorAgent) EnqueJob(name, remoteKey, targetId string) error {
	if targetId == "" {
		targetId = name
	}
	job := &pb.FeedJob{
		Id:        name,
		RemoteKey: remoteKey,
		TargetId:  targetId,
		Start:     0,
		PageSize:  100,
		Created:   time.Now().Unix(),
		Updated:   time.Now().Unix(),
	}
	_, err := ma.client.EnqueJob(context.Background(), job)
	if err != nil {
		return err
	}
	return nil
}

func (ma *MirrorAgent) newJob() error {
	feedjob, err := ma.client.GetFeedJob(context.Background(), ma.worker)
	if err != nil {
		return err
	}
	ma.worker.Job = feedjob
	return nil
}

func (ma *MirrorAgent) archive() error {
	feedjob := ma.worker.Job
	// TODO: temp change, too much job
	// set max_limit if feed not directly archived.
	if feedjob.Id != feedjob.TargetId {
		feedjob.ForceUpdate = true
		feedjob.MaxLimit = 299
	}

	httpClient := http.DefaultClient
	if config.cacheDir != "" {
		if err := os.Mkdir(config.cacheDir, 0700); err != nil && !os.IsExist(err) {
			log.Fatalf("Can not create http cache dir, %s", config.cacheDir)
		}
		userCacheDir := filepath.Join(config.cacheDir, feedjob.TargetId)
		if err := os.Mkdir(userCacheDir, 0700); err != nil && !os.IsExist(err) {
			log.Fatalf("Can not create http cache dir, %s", userCacheDir)
		}

		cache := diskcache.New(userCacheDir)
		tp := httpcache.NewTransport(cache)
		httpClient = &http.Client{Transport: tp}
		log.Printf(">>> disk cache enabled at: %s", userCacheDir)
	}
	ma.apiv1 = ff.NewV1Client(httpClient, feedjob.Id, feedjob.RemoteKey)
	ma.apiv2 = ff.NewClient(httpClient, feedjob.Id, feedjob.RemoteKey)

	var err error
	profile := new(pb.Profile)
	if feedjob.Uuid == "" {
		log.Printf("Start fetching feed: %s", feedjob.TargetId)
		profile, err = ma.profile(feedjob)
		if err != nil {
			return err
		}
		feedjob.Uuid = profile.Uuid // job.uuid matches to job.targetId not job.id
	} else {
		// id and uuid used when archive history
		profile.Uuid = feedjob.Uuid
		profile.Id = feedjob.TargetId
	}

	log.Printf("Start fetching entries: %s", profile.Id)
	var total int
	if feedjob.ForceUpdate {
		total, err = ma.forceArchiveFeed(profile, int(feedjob.MaxLimit))
	} else {
		total, err = ma.archiveFeed(profile)
	}
	if err != nil {
		return err
	}

	job, err := ma.client.FinishJob(context.Background(), feedjob)
	if err != nil {
		return err
	}
	ma.worker.Job = nil
	log.Printf("Job done %s, %d entries", job.Key, total)

	return nil
}

func (ma *MirrorAgent) resetArchivedEntries() {
	ma.archived = make(map[string]bool)
}

func (ma *MirrorAgent) archiveFeed(profile *pb.Profile) (int, error) {
	ma.resetArchivedEntries()
	// stream is Api_ArchiveFeedClient
	stream, err := ma.client.ArchiveFeed(context.Background())
	defer stream.CloseAndRecv()
	if err != nil {
		return 0, err
	}

	start := 0
	pagesize := 100

	total := 0
	for {
		n, err := ma.fetchFeedPage(stream, profile, start, pagesize)
		if err != nil || n == 0 {
			// error of finished
			return total, err
		}
		total += n
		start += n // next page
	}
}

// TODO: should embed force_update to archive_feed paramater.
func (ma *MirrorAgent) forceArchiveFeed(profile *pb.Profile, maxLimit int) (int, error) {
	if maxLimit <= 0 {
		return 0, fmt.Errorf("max_limit should > 0")
	}

	ma.resetArchivedEntries()
	// stream is Api_ArchiveFeedClient
	stream, err := ma.client.ForceArchiveFeed(context.Background())
	defer stream.CloseAndRecv()
	if err != nil {
		return 0, err
	}

	start := 0
	pagesize := 100

	total := 0
	for {
		n, err := ma.fetchFeedPage(stream, profile, start, pagesize)
		if err != nil || n == 0 {
			// error of finished
			return total, err
		}
		total += n
		if total > maxLimit {
			// done
			return total, nil
		}

		start += n // next page
	}
}

func (ma *MirrorAgent) fetchFeedPage(stream pb.Api_ArchiveFeedClient, profile *pb.Profile, start, pagesize int) (int, error) {
	opt := new(ff.FeedOptions)
	opt.Start = start
	opt.Num = pagesize
	opt.RawBody = 1

	feed, _, err := ma.apiv2.Feed(profile.Id, opt)
	if err != nil {
		return 0, err
	}
	if len(feed.Entries) == 0 {
		return 0, nil
	}

	repeat := true
	for _, entry := range feed.Entries {
		if _, ok := ma.archived[entry.Id]; !ok {
			repeat = false
			break
		}
	}

	if repeat {
		ma.resetArchivedEntries()
		return 0, nil
	}

	for i, entry := range feed.Entries {
		entry.ProfileUuid = profile.Uuid
		if err := stream.Send(entry); err != nil {
			log.Printf("%v.Send(%v) = %v", stream, entry, err)
			return i, err
		}

		ma.archived[entry.Id] = true
	}

	return len(feed.Entries), nil
}

func (ma *MirrorAgent) profile(job *pb.FeedJob) (*pb.Profile, error) {
	// get feedinfo first, so we know its type
	feedinfo, resp, err := ma.apiv2.Feedinfo(job.TargetId)
	if err != nil {
		log.Printf("Failed on request profile: %s", resp.Request.URL)
		return nil, err
	}

	if feedinfo.Type == "special" {
		return nil, fmt.Errorf(">>> we do not record special feed, skip...")
	}

	v1profile, resp, err := ma.apiv1.V1Profile(job.TargetId, feedinfo.Type)
	if err != nil {
		return nil, err
	}

	log.Printf("Construct user profile: %s(%s)", v1profile.Nickname, v1profile.Id)
	feedinfo.Uuid = v1profile.Id
	if feedinfo.Id == job.Id {
		// record remote key, so we could pull user subscription later
		feedinfo.RemoteKey = job.RemoteKey
	}
	profile, err := ma.client.PostFeedinfo(context.Background(), feedinfo)
	if err != nil {
		return nil, err
	}
	log.Printf("Profile: %s", profile)
	return profile, nil
}

func main() {
	flag.Parse()

	conn, err := grpc.Dial(config.address)
	if err != nil {
		log.Fatalf("Connection error: %v", err)
	}
	defer conn.Close()

	agent := NewMirrorAgent(conn)
	agent.Start()
}
