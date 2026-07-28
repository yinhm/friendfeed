package server

import (
	"testing"

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
