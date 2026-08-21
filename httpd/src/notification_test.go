package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
)

type captureNotificationRender struct {
	data pongo2.Context
}

func (r *captureNotificationRender) Instance(_ string, data any) render.Render {
	r.data, _ = data.(pongo2.Context)
	return render.Data{ContentType: "text/html; charset=utf-8", Data: []byte("rendered")}
}

func TestNotificationToViewUsesGroupWorkflowLinks(t *testing.T) {
	received := notificationToView(notificationRecordDTO{
		Kind:               "FOLLOW_REQUEST_RECEIVED",
		RecipientUUID:      "admin",
		TargetUUID:         "group-uuid",
		ActorNameSnapshot:  "Alice",
		TargetNameSnapshot: "Book Club",
		TargetType:         "group",
		TargetID:           "book-club",
	})
	require.Equal(t, "Alice requested to join Book Club", received.Text)
	require.Equal(t, "/groups/book-club/members", received.Href)

	approved := notificationToView(notificationRecordDTO{
		Kind:               "FOLLOW_REQUEST_APPROVED",
		TargetNameSnapshot: "Book Club",
		TargetType:         "group",
		TargetID:           "book-club",
	})
	require.Equal(t, "Your request to join Book Club was approved", approved.Text)
	require.Equal(t, "/feed/book-club", approved.Href)

	rejected := notificationToView(notificationRecordDTO{
		Kind:               "FOLLOW_REQUEST_REJECTED",
		TargetNameSnapshot: "Book Club",
		TargetType:         "group",
		TargetID:           "book-club",
	})
	require.Equal(t, "Your request to join Book Club was declined", rejected.Text)
	require.Equal(t, "/feed/book-club", rejected.Href)
}

func TestNotificationToViewUsesUserFeedLinks(t *testing.T) {
	received := notificationToView(notificationRecordDTO{
		Kind:               "FOLLOW_REQUEST_RECEIVED",
		RecipientUUID:      "owner-uuid",
		TargetUUID:         "owner-uuid",
		ActorNameSnapshot:  "Alice",
		TargetNameSnapshot: "Owner",
		TargetType:         "user",
		TargetID:           "owner",
	})
	require.Equal(t, "Alice requested to follow you", received.Text)
	require.Equal(t, "/account/requests", received.Href)

	approved := notificationToView(notificationRecordDTO{
		Kind:               "FOLLOW_REQUEST_APPROVED",
		TargetNameSnapshot: "Owner",
		TargetType:         "user",
		TargetID:           "owner",
	})
	require.Equal(t, "Your request to follow Owner was approved", approved.Text)
	require.Equal(t, "/feed/owner", approved.Href)
}

func TestNotificationToViewLinksEntryAndGroupTransitions(t *testing.T) {
	liked := notificationToView(notificationRecordDTO{
		Kind:              "ENTRY_LIKED",
		ActorNameSnapshot: "Alice",
		EntryUUID:         "entry-id",
	})
	require.Equal(t, "/e/entry-id", liked.Href)

	removed := notificationToView(notificationRecordDTO{
		Kind:               "GROUP_MEMBER_REMOVED",
		ActorNameSnapshot:  "Admin",
		TargetNameSnapshot: "Book Club",
		TargetID:           "book-club",
	})
	require.Equal(t, "Admin removed you from Book Club", removed.Text)
	require.Equal(t, "/feed/book-club", removed.Href)
}

func TestHTMLNotificationBadgeAndSummaryFailure(t *testing.T) {
	tests := []struct {
		name   string
		unread uint32
		fail   bool
		want   string
	}{
		{name: "zero"},
		{name: "one", unread: 1, want: "1"},
		{name: "ninety-nine", unread: 99, want: "99"},
		{name: "capped", unread: 100, want: "99+"},
		{name: "summary failure", fail: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeGroupClient{profile: &pb.Profile{Uuid: testGroupUserUUID, Id: "test-user", Type: "user"}}
			client.commandFunc = func(req *pb.CommandRequest) (*pb.CommandResponse, error) {
				if tc.fail {
					return nil, errors.New("summary unavailable")
				}
				return &pb.CommandResponse{Result: `{"version":1,"unread_count":` + fmt.Sprint(tc.unread) + `,"total_count":0}`}, nil
			}
			s := newGroupTestServer(client)
			router := groupTestRouter(s)
			capture := new(captureNotificationRender)
			router.HTMLRender = capture
			router.GET("/page", func(c *gin.Context) { s.HTML(c, http.StatusOK, "page.html", pongo2.Context{}) })
			cookie := groupLoginCookie(t, router)
			req := httptest.NewRequest(http.MethodGet, "/page", nil)
			req.AddCookie(cookie)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)

			require.Equal(t, http.StatusOK, response.Code)
			value, present := capture.data["notification_unread_display"]
			if tc.want == "" {
				require.False(t, present)
			} else {
				require.Equal(t, tc.want, value)
			}
		})
	}
}

func TestNotificationsHandlerMarksReadAfterSuccessfulRender(t *testing.T) {
	client := &fakeGroupClient{profile: &pb.Profile{Uuid: testGroupUserUUID, Id: "test-user", Type: "user"}}
	client.commandFunc = func(req *pb.CommandRequest) (*pb.CommandResponse, error) {
		switch req.Command {
		case "NotificationList":
			return &pb.CommandResponse{Result: `{"version":1,"items":[{"kind":"ENTRY_LIKED","actor_name_snapshot":"Alice","entry_uuid":"entry-id","activity_at_ms":1}]}`}, nil
		case "NotificationSummary":
			return &pb.CommandResponse{Result: `{"version":1,"unread_count":1,"total_count":1}`}, nil
		case "NotificationMarkRead":
			return &pb.CommandResponse{Result: `{"version":1}`}, nil
		default:
			return nil, fmt.Errorf("unexpected command %q", req.Command)
		}
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	capture := new(captureNotificationRender)
	router.HTMLRender = capture
	router.GET("/notifications", s.NotificationsHandler)
	cookie := groupLoginCookie(t, router)
	req := httptest.NewRequest(http.MethodGet, "/notifications", nil)
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, []string{"NotificationList", "NotificationSummary", "NotificationMarkRead"}, commandNames(client.commandCalls))
	views, ok := capture.data["notifications"].([]notificationView)
	require.True(t, ok)
	require.Len(t, views, 1)
	require.Equal(t, "Alice liked your post", views[0].Text)
	require.NotContains(t, views[0].Text, "body")
}

func TestNotificationsHandlerDoesNotMarkReadWhenRenderFails(t *testing.T) {
	client := &fakeGroupClient{profileErr: errors.New("profile unavailable")}
	client.commandFunc = func(req *pb.CommandRequest) (*pb.CommandResponse, error) {
		if req.Command != "NotificationList" {
			return nil, fmt.Errorf("unexpected command %q", req.Command)
		}
		return &pb.CommandResponse{Result: `{"version":1,"items":[]}`}, nil
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	router.GET("/notifications", s.NotificationsHandler)
	cookie := groupLoginCookie(t, router)
	req := httptest.NewRequest(http.MethodGet, "/notifications", nil)
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Equal(t, []string{"NotificationList"}, commandNames(client.commandCalls))
}

func TestNotificationDTOCannotExposeSourceBody(t *testing.T) {
	var record notificationRecordDTO
	require.NoError(t, json.Unmarshal([]byte(`{
		"kind":"ENTRY_COMMENTED",
		"actor_name_snapshot":"Deleted user",
		"entry_uuid":"missing-entry",
		"body":"private source body"
	}`), &record))
	view := notificationToView(record)
	require.Equal(t, "Deleted user commented on your post", view.Text)
	require.NotContains(t, view.Text, "private source body")
}

func commandNames(requests []*pb.CommandRequest) []string {
	names := make([]string, 0, len(requests))
	for _, request := range requests {
		names = append(names, request.Command)
	}
	return names
}
