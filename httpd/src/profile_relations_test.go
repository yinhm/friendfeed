package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yinhm/friendfeed/pb"
)

func TestProfileRelationsPageLoadsGraphOnlyForExplicitRouteAndSortsProfiles(t *testing.T) {
	client := &fakeGroupClient{
		feedResp: &pb.Feed{Uuid: testGroupUUID, Id: "alice", Name: "Alice", Type: "user"},
		profile:  &pb.Profile{Uuid: testGroupUserUUID, Id: "viewer", Type: "user"},
		graphResp: &pb.Graph{Following: map[string]*pb.Profile{
			"zulu":  {Id: "zulu", Name: "Zulu"},
			"alpha": {Id: "alpha", Name: "alpha"},
		}},
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	capture := &captureNotificationRender{}
	router.HTMLRender = capture
	router.GET("/feed/:name/following", s.ProfileRelationsHandler("following"))
	login := groupLoginCookie(t, router)

	req := httptest.NewRequest(http.MethodGet, "/feed/alice/following", nil)
	req.AddCookie(login)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d; want 200", recorder.Code)
	}
	if client.graphReq == nil || client.graphReq.Uuid != testGroupUUID {
		t.Fatalf("FetchGraph request=%+v", client.graphReq)
	}
	var bootstrap struct {
		Page string                   `json:"page"`
		Data profileRelationsPageData `json:"data"`
	}
	if err := json.Unmarshal([]byte(capture.data["pageBootstrap"].(string)), &bootstrap); err != nil {
		t.Fatal(err)
	}
	if bootstrap.Page != "profile-relations" || bootstrap.Data.Relation != "following" ||
		len(bootstrap.Data.Profiles) != 2 || bootstrap.Data.Profiles[0].ID != "alpha" {
		t.Fatalf("bootstrap=%+v", bootstrap)
	}
}

func TestProfileRelationsPageRejectsGroupFeeds(t *testing.T) {
	client := &fakeGroupClient{
		feedResp: &pb.Feed{Uuid: testGroupUUID, Id: "club", Type: "group"},
		profile:  &pb.Profile{Uuid: testGroupUserUUID, Id: "viewer", Type: "user"},
	}
	s := newGroupTestServer(client)
	router := groupTestRouter(s)
	router.GET("/feed/:name/followers", s.ProfileRelationsHandler("followers"))
	login := groupLoginCookie(t, router)
	req := httptest.NewRequest(http.MethodGet, "/feed/club/followers", nil)
	req.AddCookie(login)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound || client.graphReq != nil {
		t.Fatalf("status=%d graphReq=%+v", recorder.Code, client.graphReq)
	}
}
