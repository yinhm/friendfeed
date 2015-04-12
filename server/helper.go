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
	if err := fmtCollapse(req, entry); err != nil {
		return err
	}
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

func fmtCollapse(req *pb.FeedRequest, entry *pb.Entry) error {
	// collapse comments and likes
	length := len(entry.Comments)
	if req.MaxComments == 0 && length > 4 {
		collapsing := &pb.Comment{
			Body:        fmt.Sprintf("%d more comments", length-2),
			Num:         int32(length - 2),
			Placeholder: true,
		}
		entry.Comments = []*pb.Comment{entry.Comments[0], collapsing, entry.Comments[length-1]}
	}
	length = len(entry.Likes)
	if req.MaxLikes == 0 && length > 4 {
		collapsing := &pb.Like{
			Body:        fmt.Sprintf("%d other people", length-2),
			Num:         int32(length - 2),
			Placeholder: true,
		}
		entry.Likes = entry.Likes[:3]
		entry.Likes = append(entry.Likes, collapsing)
	}

	return nil
}
