package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFirstEntry(t *testing.T) {
	tests := []struct {
		name string
		feed *pb.Feed
	}{
		{name: "nil feed"},
		{name: "empty feed", feed: &pb.Feed{}},
		{name: "nil entry", feed: &pb.Feed{Entries: []*pb.Entry{nil}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry, err := firstEntry(test.feed)
			if entry != nil {
				t.Fatalf("entry = %#v; want nil", entry)
			}
			if status.Code(err) != codes.NotFound {
				t.Fatalf("error code = %s; want %s", status.Code(err), codes.NotFound)
			}
		})
	}

	want := &pb.Entry{Id: "entry-id"}
	got, err := firstEntry(&pb.Feed{Entries: []*pb.Entry{want}})
	if err != nil {
		t.Fatalf("firstEntry() error = %v", err)
	}
	if got != want {
		t.Fatalf("firstEntry() = %#v; want %#v", got, want)
	}
}

func TestCommentDeleteHandlerRejectsInvalidForm(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := &Server{}
	router := gin.New()
	router.POST("/comment/delete", server.CommentDeleteHandler)

	request := httptest.NewRequest(
		http.MethodPost,
		"/comment/delete",
		strings.NewReader("entry=entry-id"),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want %d", recorder.Code, http.StatusBadRequest)
	}
}
