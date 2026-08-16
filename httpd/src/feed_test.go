package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/yinhm/friendfeed/pb"
)

func TestRenamedFeedLocation(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		feed      *pb.Feed
		query     string
		want      string
		redirect  bool
	}{
		{
			name:      "renamed feed preserves query",
			requested: "old-name",
			feed:      &pb.Feed{Id: "new-name"},
			query:     "start=30",
			want:      "/feed/new-name?start=30",
			redirect:  true,
		},
		{
			name:      "canonical feed does not redirect",
			requested: "same-name",
			feed:      &pb.Feed{Id: "same-name"},
		},
		{
			name:      "nil feed does not redirect",
			requested: "old-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, redirect := renamedFeedLocation(tt.requested, tt.feed, tt.query)
			if got != tt.want || redirect != tt.redirect {
				t.Fatalf("renamedFeedLocation() = %q, %t; want %q, %t",
					got, redirect, tt.want, tt.redirect)
			}
		})
	}
}

func TestInteractionFeedUsesNormalFeedFormattingWithoutFilteringInteractions(t *testing.T) {
	entry := &pb.Entry{
		Id: "entry", Date: time.Now().UTC().Format(time.RFC3339),
		Likes: make([]*pb.Like, 6), Comments: make([]*pb.Comment, 6),
	}
	for i := range entry.Likes {
		entry.Likes[i] = &pb.Like{From: &pb.Feed{Id: fmt.Sprintf("like-%d", i)}}
		entry.Comments[i] = &pb.Comment{Id: fmt.Sprintf("comment-%d", i), From: &pb.Feed{Id: fmt.Sprintf("commenter-%d", i)}}
	}
	response := &pb.InteractionFeedResponse{
		Profile: &pb.Profile{Uuid: "profile", Id: "owner"},
		Items: []*pb.InteractionItem{{
			Entry: entry,
			// This identifies the indexed interaction, but must not replace the
			// complete Like list loaded on Entry.
			Like: entry.Likes[5],
		}},
	}

	feed := interactionFeedForDisplay(response, &pb.Profile{}, &pb.Graph{})
	if len(feed.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(feed.Entries))
	}
	got := feed.Entries[0]
	if len(got.Likes) != 4 || !got.Likes[3].Placeholder || got.Likes[3].Num != 3 {
		t.Fatalf("likes were not formatted from the complete list: %+v", got.Likes)
	}
	if len(got.Comments) != 3 || !got.Comments[1].Placeholder || got.Comments[1].Num != 4 {
		t.Fatalf("comments were not formatted from the complete list: %+v", got.Comments)
	}
}

func TestRenamedInteractionFeedLocationPreservesSuffixAndQuery(t *testing.T) {
	got, redirect := renamedFeedLocationWithSuffix(
		"old-name",
		&pb.Feed{Id: "new-name"},
		"comments",
		"cursor=abc",
	)
	if !redirect || got != "/feed/new-name/comments?cursor=abc" {
		t.Fatalf("renamedFeedLocationWithSuffix() = %q, %t", got, redirect)
	}
}
