package server

import (
	"encoding/json"

	"github.com/flosch/pongo2"
	"github.com/yinhm/friendfeed/pb"
)

const browserBootstrapVersion = 1

// pageBootstrap is the versioned envelope used by the dispatcher introduced
// in the next migration checkpoint. Feed keeps its legacy top-level JSON shape
// until that dispatcher is present, but both envelope and payload are explicit
// DTOs rather than protobuf or template context.
type pageBootstrap struct {
	Version     int             `json:"version"`
	Page        string          `json:"page"`
	CurrentUser *profileSummary `json:"current_user,omitempty"`
	Layout      *layoutView     `json:"layout,omitempty"`
	Data        any             `json:"data"`
}

type layoutGroupView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Private bool   `json:"private,omitempty"`
}

type layoutArchiveYearView struct {
	Year   int32  `json:"year"`
	Count  int64  `json:"count"`
	Cursor string `json:"cursor,omitempty"`
}

type layoutView struct {
	OnPage        bool                    `json:"onpage"`
	HasUnread     bool                    `json:"has_unread_notifications"`
	Groups        []layoutGroupView       `json:"groups,omitempty"`
	ShowGroups    bool                    `json:"show_groups"`
	ArchiveFeedID string                  `json:"archive_feed_id,omitempty"`
	ArchiveYears  []layoutArchiveYearView `json:"archive_years,omitempty"`
}

type profileSummary struct {
	UUID    string `json:"uuid"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Picture string `json:"picture,omitempty"`
}

// feedPageData is the explicit Browser-BFF boundary. Keep JSON names stable
// while the React migration proceeds; protobuf messages and arbitrary template
// context must never cross this boundary directly.
type feedPageData struct {
	Feed              feedView `json:"feed"`
	ShowHeader        bool     `json:"show_header"`
	ShowPaging        bool     `json:"show_paging"`
	ShowShare         bool     `json:"show_share"`
	ShowPrev          bool     `json:"show_prev"`
	ShowNext          bool     `json:"show_next"`
	PrevStart         int32    `json:"prev_start"`
	NextStart         int32    `json:"next_start"`
	CursorPaging      bool     `json:"cursor_paging"`
	NextCursor        string   `json:"next_cursor"`
	RealtimeEnabled   bool     `json:"realtime_enabled"`
	RealtimeHome      bool     `json:"realtime_home"`
	OnPage            bool     `json:"onpage"`
	OnPageEdit        bool     `json:"onpage_edit"`
	Query             string   `json:"query"`
	ManageServicesURL string   `json:"manage_services_url,omitempty"`
	GroupSettingsURL  string   `json:"group_settings_url,omitempty"`
	GroupMembersURL   string   `json:"group_members_url,omitempty"`
}

type feedView struct {
	ID          string      `json:"id"`
	UUID        string      `json:"uuid"`
	Name        string      `json:"name,omitempty"`
	Picture     string      `json:"picture,omitempty"`
	Description string      `json:"description,omitempty"`
	Private     bool        `json:"private,omitempty"`
	Commands    []string    `json:"commands,omitempty"`
	Entries     []entryView `json:"entries"`
}

type feedRefView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Picture string `json:"picture,omitempty"`
	Private bool   `json:"private,omitempty"`
}
type thumbnailView struct {
	Width  int32  `json:"width,omitempty"`
	Height int32  `json:"height,omitempty"`
	Link   string `json:"link,omitempty"`
	URL    string `json:"url"`
}
type fileView struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	Size int64  `json:"size,omitempty"`
}
type commentView struct {
	ID          string       `json:"id,omitempty"`
	Body        string       `json:"body"`
	RawBody     string       `json:"rawBody,omitempty"`
	Date        string       `json:"date,omitempty"`
	Placeholder bool         `json:"placeholder,omitempty"`
	Commands    []string     `json:"commands,omitempty"`
	From        *feedRefView `json:"from,omitempty"`
}
type likeView struct {
	Placeholder bool         `json:"placeholder,omitempty"`
	Body        string       `json:"body,omitempty"`
	From        *feedRefView `json:"from,omitempty"`
}
type viaView struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}
type entryView struct {
	ID         string          `json:"id"`
	Title      string          `json:"title,omitempty"`
	Body       string          `json:"body"`
	RawBody    string          `json:"rawBody,omitempty"`
	Type       string          `json:"type,omitempty"`
	Date       string          `json:"date,omitempty"`
	From       *feedRefView    `json:"from"`
	To         []*feedRefView  `json:"to,omitempty"`
	Via        *viaView        `json:"via,omitempty"`
	Commands   []string        `json:"commands"`
	Thumbnails []thumbnailView `json:"thumbnails,omitempty"`
	Files      []fileView      `json:"files,omitempty"`
	Comments   []commentView   `json:"comments,omitempty"`
	Likes      []likeView      `json:"likes,omitempty"`
}

func feedPageDataFromContext(data pongo2.Context) feedPageData {
	feed, _ := data["feed"].(*pb.Feed)
	return feedPageData{
		Feed: feedViewFromProto(feed), ShowHeader: contextBool(data, "show_header"),
		ShowPaging: contextBool(data, "show_paging"), ShowShare: contextBool(data, "show_share"),
		ShowPrev: contextBool(data, "show_prev"), ShowNext: contextBool(data, "show_next"),
		PrevStart: contextInt32(data, "prev_start"), NextStart: contextInt32(data, "next_start"),
		CursorPaging: contextBool(data, "cursor_paging"), NextCursor: contextString(data, "next_cursor"),
		RealtimeEnabled: contextBool(data, "realtime_enabled"), RealtimeHome: contextBool(data, "realtime_home"),
		OnPage: contextBool(data, "onpage"), OnPageEdit: contextBool(data, "onpage_edit"), Query: contextString(data, "query"),
		ManageServicesURL: contextString(data, "manage_services_url"), GroupSettingsURL: contextString(data, "group_settings_url"),
		GroupMembersURL: contextString(data, "group_members_url"),
	}
}

func feedViewFromProto(feed *pb.Feed) feedView {
	if feed == nil {
		return feedView{Entries: []entryView{}}
	}
	v := feedView{ID: feed.Id, UUID: feed.Uuid, Name: feed.Name, Picture: feed.Picture, Description: feed.Description, Private: feed.Private, Commands: append([]string(nil), feed.Commands...), Entries: make([]entryView, 0, len(feed.Entries))}
	for _, entry := range feed.Entries {
		if entry != nil {
			v.Entries = append(v.Entries, entryViewFromProto(entry))
		}
	}
	return v
}

func feedRefViewFromProto(ref *pb.Feed) *feedRefView {
	if ref == nil {
		return nil
	}
	return &feedRefView{ID: ref.Id, Name: ref.Name, Picture: ref.Picture, Private: ref.Private}
}
func entryViewFromProto(e *pb.Entry) entryView {
	v := entryView{ID: e.Id, Title: e.Title, Body: e.Body, Type: e.Type, Date: e.Date, From: feedRefViewFromProto(e.From), Commands: append([]string(nil), e.Commands...)}
	if hasCommand(e.Commands, "edit") {
		v.RawBody = e.RawBody
	}
	for _, x := range e.To {
		v.To = append(v.To, feedRefViewFromProto(x))
	}
	if e.Via != nil {
		v.Via = &viaView{Name: e.Via.Name, URL: e.Via.Url}
	}
	for _, x := range e.Thumbnails {
		if x != nil {
			v.Thumbnails = append(v.Thumbnails, thumbnailView{Width: x.Width, Height: x.Height, Link: x.Link, URL: x.Url})
		}
	}
	for _, x := range e.Files {
		if x != nil {
			v.Files = append(v.Files, fileView{URL: x.Url, Name: x.Name, Type: x.Type, Size: int64(x.Size)})
		}
	}
	for _, x := range e.Comments {
		if x != nil {
			comment := commentView{ID: x.Id, Body: x.Body, Date: x.Date, Placeholder: x.Placeholder, Commands: append([]string(nil), x.Commands...), From: feedRefViewFromProto(x.From)}
			if hasCommand(x.Commands, "edit") {
				comment.RawBody = x.RawBody
			}
			v.Comments = append(v.Comments, comment)
		}
	}
	for _, x := range e.Likes {
		if x != nil {
			v.Likes = append(v.Likes, likeView{Placeholder: x.Placeholder, Body: x.Body, From: feedRefViewFromProto(x.From)})
		}
	}
	return v
}

func hasCommand(commands []string, want string) bool {
	for _, command := range commands {
		if command == want {
			return true
		}
	}
	return false
}

func contextBool(c pongo2.Context, k string) bool     { v, _ := c[k].(bool); return v }
func contextString(c pongo2.Context, k string) string { v, _ := c[k].(string); return v }
func contextInt32(c pongo2.Context, k string) int32   { v, _ := c[k].(int32); return v }
func marshalFeedPageData(data pongo2.Context) ([]byte, error) {
	return marshalPageBootstrap("feed", feedPageDataFromContext(data))
}

func marshalPageBootstrap(page string, data any) ([]byte, error) {
	return json.Marshal(pageBootstrap{Version: browserBootstrapVersion, Page: page, Data: data})
}

func enrichPageBootstrap(raw string, profile *pb.Profile, context pongo2.Context) (string, error) {
	// Keep page data as raw JSON. Decoding into any would round large integer
	// fields through float64 while merely adding layout metadata.
	var bootstrap struct {
		Version     int             `json:"version"`
		Page        string          `json:"page"`
		CurrentUser *profileSummary `json:"current_user,omitempty"`
		Layout      *layoutView     `json:"layout,omitempty"`
		Data        json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &bootstrap); err != nil {
		return "", err
	}
	if profile != nil && profile.Uuid != "" {
		bootstrap.CurrentUser = &profileSummary{UUID: profile.Uuid, ID: profile.Id, Name: profile.Name, Picture: PictureOrDefault(profile.Picture)}
	}
	layout := &layoutView{
		OnPage: contextBool(context, "onpage"), HasUnread: contextBool(context, "has_unread_notifications"),
		ShowGroups: contextBool(context, "show_groups_sidebar"), ArchiveFeedID: contextString(context, "feed_archive_id"),
	}
	if groups, ok := context["user_groups"].([]*pb.Profile); ok {
		for _, group := range groups {
			if group != nil {
				layout.Groups = append(layout.Groups, layoutGroupView{ID: group.Id, Name: group.Name, Private: group.Private})
			}
		}
	}
	if archive, ok := context["feed_archive"].(*pb.FeedArchiveStats); ok && archive != nil {
		for _, year := range archive.Years {
			if year != nil {
				layout.ArchiveYears = append(layout.ArchiveYears, layoutArchiveYearView{Year: year.Year, Count: year.EntryCount, Cursor: year.Cursor})
			}
		}
	}
	bootstrap.Layout = layout
	encoded, err := json.Marshal(bootstrap)
	return string(encoded), err
}
