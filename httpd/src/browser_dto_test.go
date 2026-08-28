package server

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/flosch/pongo2"
	"github.com/yinhm/ffdb/pb"
)

func TestFeedPageDataIsAnExplicitSafeBrowserContract(t *testing.T) {
	raw, err := marshalFeedPageData(pongo2.Context{
		"feed": &pb.Feed{Id: "alice", Uuid: "feed-uuid", Entries: []*pb.Entry{{
			Id: "entry", Body: `</script><script>alert(1)</script>`,
			From: &pb.Feed{Id: "alice", Name: "Alice"}, Commands: []string{"comment"},
		}}},
		"show_header":    true,
		"session":        "must-not-cross",
		"oauth_token":    "must-not-cross",
		"internal_error": "must-not-cross",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{[]byte("must-not-cross"), []byte("</script>"), []byte("oauth_token"), []byte("internal_error")} {
		if bytes.Contains(raw, forbidden) {
			t.Fatalf("browser JSON contains forbidden value %q: %s", forbidden, raw)
		}
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["show_header"] != true {
		t.Fatalf("show_header=%v", got["show_header"])
	}
	feed := got["feed"].(map[string]any)
	if feed["id"] != "alice" || feed["uuid"] != "feed-uuid" {
		t.Fatalf("feed=%v", feed)
	}
}
