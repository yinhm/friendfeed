package server

import (
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
	entry.FormatComments(req.MaxComments)
}

func fmtLikes(req *pb.FeedRequest, entry *pb.Entry) {
	entry.FormatLikes(req.MaxLikes)
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
