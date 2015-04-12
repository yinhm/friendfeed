package server

import (
	"fmt"

	pb "github.com/yinhm/friendfeed/proto"
	store "github.com/yinhm/friendfeed/storage"
)

func FormatFeedEntry(mdb *store.Store, req *pb.FeedRequest, entry *pb.Entry) error {
	if err := fmtEntryProfile(mdb, entry); err != nil {
		return err
	}
	fmtComments(req, entry)
	fmtLikes(req, entry)
	return nil
}

func FormatEntry(mdb *store.Store, req *pb.FeedRequest, entry *pb.Entry) error {
	if err := fmtEntryProfile(mdb, entry); err != nil {
		return err
	}
	fmtLikes(req, entry)
	return nil
}

func fmtEntryProfile(mdb *store.Store, entry *pb.Entry) error {
	// refetch user profile
	profile, err := store.GetProfile(mdb, entry.From.Id)
	if err != nil {
		return err
	}
	entry.From.Picture = profile.Picture
	return nil
}

func fmtComments(req *pb.FeedRequest, entry *pb.Entry) {
	// collapse comments
	length := len(entry.Comments)
	if req.MaxComments == 0 && length > 4 {
		collapsing := &pb.Comment{
			Body:        fmt.Sprintf("%d more comments", length-2),
			Num:         int32(length - 2),
			Placeholder: true,
		}
		entry.Comments = []*pb.Comment{entry.Comments[0], collapsing, entry.Comments[length-1]}
	}
}

func fmtLikes(req *pb.FeedRequest, entry *pb.Entry) {
	// collapse likes
	length := len(entry.Likes)
	if req.MaxLikes == 0 && length > 4 {
		collapsing := &pb.Like{
			Body:        fmt.Sprintf("%d other people", length-2),
			Num:         int32(length - 2),
			Placeholder: true,
		}
		entry.Likes = entry.Likes[:3]
		entry.Likes = append(entry.Likes, collapsing)
	}
}

func BuildGraph(info *pb.Feedinfo) *pb.Graph {
	graph := &pb.Graph{
		Subscriptions: make(map[string]*pb.Profile),
		Admins:        make(map[string]*pb.Profile),
		Services:      make(map[string]*pb.Service),
	}
	for _, item := range info.Subscriptions {
		graph.Subscriptions[item.Id] = item
	}
	for _, item := range info.Admins {
		graph.Admins[item.Id] = item
	}
	for _, item := range info.Services {
		graph.Services[item.Id] = item
	}
	return graph
}
