package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
)

type notificationRecordDTO struct {
	Version            uint32 `json:"version"`
	ID                 string `json:"id"`
	Kind               string `json:"kind"`
	RecipientUUID      string `json:"recipient_uuid"`
	ActorUUID          string `json:"actor_uuid,omitempty"`
	TargetUUID         string `json:"target_uuid,omitempty"`
	EntryUUID          string `json:"entry_uuid,omitempty"`
	CommentUUID        string `json:"comment_uuid,omitempty"`
	RequestedAt        string `json:"requested_at,omitempty"`
	ActivityAtMS       int64  `json:"activity_at_ms"`
	CreatedAtNS        int64  `json:"created_at_ns"`
	ActorNameSnapshot  string `json:"actor_name_snapshot,omitempty"`
	TargetNameSnapshot string `json:"target_name_snapshot,omitempty"`
	TargetType         string `json:"target_type,omitempty"`
	TargetID           string `json:"target_id,omitempty"`
}

type notificationPageDTO struct {
	Version    uint32                  `json:"version"`
	Items      []notificationRecordDTO `json:"items"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

type notificationSummaryDTO struct {
	Version     uint32 `json:"version"`
	UnreadCount uint32 `json:"unread_count"`
	TotalCount  uint32 `json:"total_count"`
}

type notificationView struct {
	Text string
	Href string
	Date string
}

func (s *Server) notificationSummary(ctx context.Context, userUUID string) (notificationSummaryDTO, error) {
	resp, err := s.client.Command(ctx, &pb.CommandRequest{
		Command: "NotificationSummary",
		Arg1:    userUUID,
	})
	if err != nil {
		return notificationSummaryDTO{}, err
	}
	var summary notificationSummaryDTO
	if err := json.Unmarshal([]byte(resp.Result), &summary); err != nil {
		return notificationSummaryDTO{}, err
	}
	return summary, nil
}

func notificationActor(record notificationRecordDTO) string {
	if record.ActorNameSnapshot != "" {
		return record.ActorNameSnapshot
	}
	return "Someone"
}

func notificationTarget(record notificationRecordDTO) string {
	if record.TargetNameSnapshot != "" {
		return record.TargetNameSnapshot
	}
	return "this feed"
}

func notificationFeedHref(record notificationRecordDTO) string {
	if record.TargetID == "" {
		return "/notifications"
	}
	return "/feed/" + url.PathEscape(record.TargetID)
}

func notificationGroupMembersHref(record notificationRecordDTO) string {
	if record.TargetID == "" {
		return "/groups"
	}
	return "/groups/" + url.PathEscape(record.TargetID) + "/members"
}

func notificationToView(record notificationRecordDTO) notificationView {
	actor := notificationActor(record)
	target := notificationTarget(record)
	view := notificationView{
		Text: "Notification",
		Href: "/notifications",
		Date: time.UnixMilli(record.ActivityAtMS).UTC().Format("2006-01-02 15:04"),
	}
	switch record.Kind {
	case "FOLLOW_REQUEST_RECEIVED":
		if record.TargetType == "group" || record.TargetUUID != record.RecipientUUID {
			view.Text = fmt.Sprintf("%s requested to join %s", actor, target)
			view.Href = notificationGroupMembersHref(record)
		} else {
			view.Text = fmt.Sprintf("%s requested to follow you", actor)
			view.Href = "/account/requests"
		}
	case "FOLLOW_REQUEST_APPROVED":
		if record.TargetType == "group" {
			view.Text = fmt.Sprintf("Your request to join %s was approved", target)
		} else {
			view.Text = fmt.Sprintf("Your request to follow %s was approved", target)
		}
		view.Href = notificationFeedHref(record)
	case "FOLLOW_REQUEST_REJECTED":
		if record.TargetType == "group" {
			view.Text = fmt.Sprintf("Your request to join %s was declined", target)
		} else {
			view.Text = fmt.Sprintf("Your request to follow %s was declined", target)
		}
		view.Href = notificationFeedHref(record)
	case "ENTRY_COMMENTED":
		view.Text = fmt.Sprintf("%s commented on your post", actor)
		if record.EntryUUID != "" {
			view.Href = "/e/" + url.PathEscape(record.EntryUUID)
		}
	case "ENTRY_LIKED":
		view.Text = fmt.Sprintf("%s liked your post", actor)
		if record.EntryUUID != "" {
			view.Href = "/e/" + url.PathEscape(record.EntryUUID)
		}
	case "GROUP_ADMIN_ADDED":
		view.Text = fmt.Sprintf("%s promoted you to admin of %s", actor, target)
		view.Href = notificationFeedHref(record)
	case "GROUP_ADMIN_REMOVED":
		view.Text = fmt.Sprintf("%s removed you as admin of %s", actor, target)
		view.Href = notificationFeedHref(record)
	case "GROUP_MEMBER_REMOVED":
		view.Text = fmt.Sprintf("%s removed you from %s", actor, target)
		view.Href = notificationFeedHref(record)
	}
	return view
}

func (s *Server) NotificationsHandler(c *gin.Context) {
	userUUID := CurrentUserUuid(c)
	if userUUID == "" {
		c.Redirect(http.StatusSeeOther, "/")
		return
	}
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	resp, err := s.client.Command(ctx, &pb.CommandRequest{
		Command: "NotificationList",
		Arg1:    userUUID,
		Arg2:    c.Query("cursor"),
	})
	if RequestError(c, err) {
		return
	}
	var page notificationPageDTO
	if err := json.Unmarshal([]byte(resp.Result), &page); err != nil {
		c.String(http.StatusInternalServerError, "Server error.")
		return
	}
	items := make([]notificationView, 0, len(page.Items))
	for _, record := range page.Items {
		items = append(items, notificationToView(record))
	}

	s.HTML(c, http.StatusOK, "notifications.html", pongo2.Context{
		"title":         "Notifications",
		"notifications": items,
		"next_cursor":   page.NextCursor,
	})
	if c.Writer.Status() >= http.StatusBadRequest {
		return
	}

	// Mark-all-read is intentionally best effort and happens only after the
	// page rendered successfully. Use a fresh timeout because list/render work
	// may have consumed most of the request's first RPC budget.
	markCtx, markCancel := DefaultTimeoutContext()
	defer markCancel()
	_, _ = s.client.Command(markCtx, &pb.CommandRequest{
		Command: "NotificationMarkRead",
		Arg1:    userUUID,
	})
}
