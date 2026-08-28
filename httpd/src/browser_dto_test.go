package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flosch/pongo2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
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
	if got["version"] != float64(browserBootstrapVersion) || got["page"] != "feed" {
		t.Fatalf("bootstrap=%v", got)
	}
	data := got["data"].(map[string]any)
	if data["show_header"] != true {
		t.Fatalf("show_header=%v", data["show_header"])
	}
	feed := data["feed"].(map[string]any)
	if feed["id"] != "alice" || feed["uuid"] != "feed-uuid" {
		t.Fatalf("feed=%v", feed)
	}
}

func TestRenderFeedJSONUsesBrowserDTO(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("ffdbsess", cookie.NewStore([]byte("test-secret"))))
	router.GET("/", func(c *gin.Context) {
		(&Server{}).renderFeed(c, pongo2.Context{
			"feed": &pb.Feed{Entries: []*pb.Entry{{Id: "entry", RawBody: "hidden", From: &pb.Feed{}}}},
		})
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("hidden")) || bytes.Contains(recorder.Body.Bytes(), []byte("ProfileUuid")) {
		t.Fatalf("JSON refresh bypassed DTO: %s", recorder.Body.String())
	}
	var got feedPageData
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Feed.Entries) != 1 || got.Feed.Entries[0].ID != "entry" {
		t.Fatalf("feed DTO=%+v", got.Feed)
	}
}

func TestFeedPageDataOnlyExposesEditableRawBody(t *testing.T) {
	view := feedViewFromProto(&pb.Feed{Entries: []*pb.Entry{
		{Id: "read-only", RawBody: "private editor state"},
		{Id: "editable", RawBody: "editable state", Commands: []string{"edit"}, Comments: []*pb.Comment{
			{Id: "read-only-comment", RawBody: "hidden"},
			{Id: "editable-comment", RawBody: "shown", Commands: []string{"edit"}},
		}},
	}})
	if view.Entries[0].RawBody != "" {
		t.Fatal("read-only Entry exposed rawBody")
	}
	if view.Entries[1].RawBody != "editable state" {
		t.Fatal("editable Entry lost rawBody")
	}
	if view.Entries[1].Comments[0].RawBody != "" || view.Entries[1].Comments[1].RawBody != "shown" {
		t.Fatalf("comment rawBody filtering failed: %+v", view.Entries[1].Comments)
	}
}
