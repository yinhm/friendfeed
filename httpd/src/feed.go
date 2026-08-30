package server

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (s *Server) FetchFeed(c *gin.Context, req proto.Message) (profile *pb.Profile, feed *pb.Feed, err error) {
	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	var format = false

	switch r := req.(type) {
	case *pb.FeedRequest:
		feed, err = s.client.FetchFeed(ctx, r)
		format = true
	case *pb.EntryRequest:
		feed, err = s.client.FetchEntry(ctx, r)
	case *pb.SearchRequest:
		feed, err = s.client.Search(ctx, r)
		format = true
	}
	if err != nil {
		return
	}
	profile, err = s.CurrentUser(c)
	if err != nil {
		return
	}

	// IMPORTANT:
	// rewrite public uuid to current user uuid
	if feed.Uuid == "Public" {
		feed.Uuid = profile.Uuid
	}

	// check user.IsFollowed(feed)
	if _, ok := req.(*pb.FeedRequest); ok {
		s.BuildFollow(profile, feed)
	}

	graph, err := s.CurrentGraph(c)
	if err != nil {
		return
	}
	for _, e := range feed.Entries {
		prepareFeedEntry(e, profile, graph, format)
	}
	return
}

// prepareFeedEntry is the shared httpd presentation boundary for every feed
// page. RPCs choose which entries belong to a feed; collapsing interactions,
// formatting time/body and rebuilding commands must not vary by feed kind.
func prepareFeedEntry(entry *pb.Entry, profile *pb.Profile, graph *pb.Graph, format bool) {
	entry.RebuildCommand(profile, graph)
	if basetime, err := time.Parse(time.RFC3339, entry.Date); err == nil {
		entry.Date = util.FormatTime(basetime)
	}
	if format {
		entry.FormatComments(0)
		entry.FormatLikes(0)
		ellipsis := fmt.Sprintf("<a href=\"/e/%s\" style=\"padding-left: 30px;\">Read more...</a>", entry.Id)
		entry.Body = util.Truncate(entry.Body, 300, ellipsis)
		entry.Body = collapseThumbnailImages(entry.Body, entry.Thumbnails)
	} else {
		// The permalink page renders the full body, which may carry deliberate
		// layout. Thumbnails already inline in the body would only duplicate,
		// but legacy entries (RSS/archive imports) keep their images in
		// thumbnails alone, so only the redundant ones are dropped.
		entry.Thumbnails = thumbnailsNotInBody(entry.Body, entry.Thumbnails)
	}
	entry.RebuildCommentsCommand(profile, graph)
}

// thumbnailsNotInBody keeps the thumbnails the body does not already render
// inline, matched by exact canonical URL.
func thumbnailsNotInBody(body string, thumbnails []*pb.Thumbnail) []*pb.Thumbnail {
	if !strings.Contains(body, "<img") && !strings.Contains(body, "<a") {
		return thumbnails
	}
	kept := thumbnails[:0]
	for _, thumbnail := range thumbnails {
		if thumbnail == nil {
			continue
		}
		inline := thumbnail.Url != "" && strings.Contains(body, thumbnail.Url)
		inline = inline || thumbnail.Link != "" && strings.Contains(body, thumbnail.Link)
		if !inline {
			kept = append(kept, thumbnail)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// collapseThumbnailImages removes body images that the entry thumbnails
// already represent, so list pages show each image once, in the media box.
// An <a> wrapper pointing at the same thumbnail's original is removed
// together with the image it wraps, and paragraphs left visually empty —
// by the collapse or by the editor's zero-width image spacers — are dropped
// as well. Unmatched (e.g. unmigrated remote) images stay inline. Bodies
// are sanitized fragments, so a parse failure conservatively keeps the body
// unchanged.
func collapseThumbnailImages(body string, thumbnails []*pb.Thumbnail) string {
	if len(thumbnails) == 0 || !strings.Contains(body, "<img") {
		return body
	}
	urls := make(map[string]bool, 2*len(thumbnails))
	for _, thumbnail := range thumbnails {
		if thumbnail == nil {
			continue
		}
		if thumbnail.Url != "" {
			urls[thumbnail.Url] = true
		}
		if thumbnail.Link != "" {
			urls[thumbnail.Link] = true
		}
	}
	context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(body), context)
	if err != nil {
		return body
	}
	// Reparent under a synthetic root so the collapse rules apply to
	// top-level nodes as well.
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	var collapse func(node *html.Node)
	collapse = func(node *html.Node) {
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			collapse(child)
			switch {
			case child.Type == html.ElementNode && child.Data == "img" &&
				urls[htmlAttr(child, "src")]:
				node.RemoveChild(child)
			case child.Type == html.ElementNode && child.Data == "a" &&
				urls[htmlAttr(child, "href")] && emptyContent(child):
				// An anchor whose only content was a collapsed image.
				node.RemoveChild(child)
			case child.Type == html.ElementNode && child.Data == "p" &&
				visuallyEmpty(child):
				node.RemoveChild(child)
			}
			child = next
		}
	}
	collapse(root)
	var out strings.Builder
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&out, child); err != nil {
			return body
		}
	}
	return out.String()
}

// emptyContent reports whether a node holds nothing but whitespace text.
func emptyContent(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode && strings.TrimSpace(child.Data) == "" {
			continue
		}
		return false
	}
	return true
}

// visuallyEmptyMediaTags are elements that render meaningful content even
// without any text.
var visuallyEmptyMediaTags = map[string]bool{
	"img": true, "video": true, "iframe": true, "embed": true,
	"audio": true, "object": true, "svg": true,
}

// visuallyEmpty reports whether a subtree renders nothing: no media elements
// and no text beyond whitespace and the zero-width characters (\u200b,
// \ufeff) the editor uses as empty-paragraph placeholders.
func visuallyEmpty(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		switch child.Type {
		case html.TextNode:
			if strings.Trim(child.Data, " \t\r\n\u00a0\u200b\ufeff") != "" {
				return false
			}
		case html.ElementNode:
			if visuallyEmptyMediaTags[child.Data] || !visuallyEmpty(child) {
				return false
			}
		}
	}
	return true
}

func htmlAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func feedContext(feed *pb.Feed, start, pageSize int32) pongo2.Context {
	prevStart := max(start-pageSize, 0)
	// The lookahead entry (one more than pageSize) signals a following page;
	// the previous page depends only on the current offset. Splitting the two
	// keeps Prev reachable on a short last page.
	showNext := len(feed.Entries) > int(pageSize)
	if showNext {
		feed.Entries = feed.Entries[:pageSize]
	}
	showPrev := start > 0
	return pongo2.Context{
		"title":       feed.Id,
		"name":        feed.Id,
		"feed":        feed,
		"prev_start":  prevStart,
		"next_start":  start + pageSize,
		"show_prev":   showPrev,
		"show_next":   showNext,
		"show_paging": showPrev || showNext,
		"show_share":  false,
	}
}

func cursorFeedContext(feed *pb.Feed) pongo2.Context {
	return pongo2.Context{
		"title":         feed.Id,
		"name":          feed.Id,
		"feed":          feed,
		"prev_start":    int32(0),
		"next_start":    int32(0),
		"next_cursor":   feed.NextCursor,
		"cursor_paging": true,
		"show_paging":   feed.NextCursor != "",
		"show_share":    false,
	}
}

// legacyFeedCursorContext renders the requested offset page once, then emits
// only the cursor link returned for the following page. Direct feed legacy
// responses retain one lookahead entry, which is not part of the page.
func legacyFeedCursorContext(feed *pb.Feed, pageSize int32) pongo2.Context {
	if len(feed.Entries) > int(pageSize) {
		feed.Entries = feed.Entries[:pageSize]
	}
	return cursorFeedContext(feed)
}

// configureFeedPagination preserves explicit legacy ?start=N links. Cursor
// pagination is the default for new profile/timeline requests, and wins when
// both styles are present.
func configureFeedPagination(r *http.Request, req *pb.FeedRequest) bool {
	query := r.URL.Query()
	cursor := query.Get("cursor")
	if cursor != "" || !query.Has("start") {
		req.Cursor = cursor
		req.CursorPaging = true
		return true
	}
	req.Start = int32(ParseStart(r))
	return false
}

// redirectAnonymousLegacyFeedStart prevents bots and anonymous readers from
// exercising the O(Start) compatibility scan. Logged-in users may render one
// legacy page, whose next link switches permanently to cursor pagination.
// Search and tag results keep their independent offset protocol.
func redirectAnonymousLegacyFeedStart(c *gin.Context) bool {
	query := c.Request.URL.Query()
	if !query.Has("start") || CurrentUserUuid(c) != "" {
		return false
	}

	query.Del("start")
	query.Del("cursor")
	location := c.Request.URL.Path
	if encoded := query.Encode(); encoded != "" {
		location += "?" + encoded
	}
	c.Redirect(http.StatusFound, location)
	return true
}

func renamedFeedLocation(requestedID string, feed *pb.Feed, rawQuery string) (string, bool) {
	return renamedFeedLocationWithSuffix(requestedID, feed, "", rawQuery)
}

func renamedFeedLocationWithSuffix(requestedID string, feed *pb.Feed, suffix, rawQuery string) (string, bool) {
	if feed == nil || feed.Id == "" || feed.Id == requestedID {
		return "", false
	}
	location := "/feed/" + url.PathEscape(feed.Id)
	if suffix == "likes" || suffix == "comments" || suffix == "groups" {
		location += "/" + suffix
	}
	if rawQuery != "" {
		location += "?" + rawQuery
	}
	return location, true
}

func (s *Server) InteractionFeedHandler(kind pb.InteractionKind, suffix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		baseReq := &pb.FeedRequest{
			Id:           name,
			PageSize:     1,
			CursorPaging: true,
			ViewerUuid:   CurrentUserUuid(c),
		}
		_, base, err := s.FetchFeed(c, baseReq)
		if RequestError(c, err) {
			return
		}
		if location, renamed := renamedFeedLocationWithSuffix(name, base, suffix, c.Request.URL.RawQuery); renamed {
			c.Redirect(http.StatusFound, location)
			return
		}
		ctx, cancel := DefaultTimeoutContext()
		defer cancel()
		response, err := s.client.FetchInteractionFeed(ctx, &pb.InteractionFeedRequest{
			ProfileUuid: base.Uuid, ViewerUuid: CurrentUserUuid(c), Kind: kind,
			Cursor: c.Query("cursor"), PageSize: 30,
		})
		if RequestError(c, err) {
			return
		}
		profile, err := s.CurrentUser(c)
		if RequestError(c, err) {
			return
		}
		graph, err := s.CurrentGraph(c)
		if RequestError(c, err) {
			return
		}
		feed := interactionFeedForDisplay(response, profile, graph)
		data := cursorFeedContext(feed)
		data["show_header"] = true
		data["title"] = feed.Id + " " + suffix
		s.renderFeed(c, data)
	}
}

func interactionFeedForDisplay(response *pb.InteractionFeedResponse, profile *pb.Profile, graph *pb.Graph) *pb.Feed {
	feed := &pb.Feed{
		Uuid: response.Profile.Uuid, Id: response.Profile.Id, Name: response.Profile.Name,
		Picture: response.Profile.Picture, Description: response.Profile.Description,
		Type: response.Profile.Type, Private: response.Profile.Private, NextCursor: response.NextCursor,
	}
	for _, item := range response.Items {
		if item == nil || item.Entry == nil {
			continue
		}
		// Keep the complete hydrated interaction lists on the Entry. The
		// item-level Like/latest Comment identifies why the Entry is indexed;
		// it is not a display filter.
		prepareFeedEntry(item.Entry, profile, graph, true)
		feed.Entries = append(feed.Entries, item.Entry)
	}
	return feed
}

func (s *Server) HomeHandler(c *gin.Context) {
	userUuid := CurrentUserUuid(c)
	if userUuid == "" {
		http.Redirect(c.Writer, c.Request, "/public", http.StatusFound)
		return
	}

	req := &pb.FeedRequest{
		Id:          "home",
		ProfileUuid: userUuid,
		PageSize:    30,
		ViewerUuid:  userUuid,
	}
	if redirectAnonymousLegacyFeedStart(c) {
		return
	}
	cursorPaging := configureFeedPagination(c.Request, req)
	legacyStart := !cursorPaging

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		log.Printf("fetch feed error: %v", err)
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	if cursorPaging {
		data = cursorFeedContext(feed)
	} else if legacyStart {
		data = legacyFeedCursorContext(feed, req.PageSize)
	}
	data["show_share"] = s.feedWritable(c, feed.Uuid)
	data["show_groups_sidebar"] = true
	// Realtime belongs only to the newest Home page. The existing cursor is
	// an older-page position, never a "since" token.
	data["realtime_home"] = cursorPaging && req.Cursor == "" && req.Start == 0
	s.renderFeed(c, data)
}

func (s *Server) FeedHandler(c *gin.Context) {
	feedname := c.Params.ByName("name")
	req := &pb.FeedRequest{
		Id:         feedname,
		PageSize:   30,
		ViewerUuid: CurrentUserUuid(c),
	}
	if redirectAnonymousLegacyFeedStart(c) {
		return
	}
	cursorPaging := configureFeedPagination(c.Request, req)
	legacyStart := !cursorPaging
	_, feed, err := s.FetchFeed(c, req)
	if err != nil && status.Code(err) == codes.PermissionDenied {
		// Private feed the viewer may not read: offer the follow-request
		// entry point instead of a bare 403.
		s.renderPrivateFeedRequest(c, feedname, "")
		return
	}
	if RequestError(c, err) {
		return
	}
	if location, renamed := renamedFeedLocation(feedname, feed, c.Request.URL.RawQuery); renamed {
		// Rename metadata is periodically reclaimed, so do not let clients
		// cache this redirect permanently after the old ID becomes reusable.
		c.Redirect(http.StatusFound, location)
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	if cursorPaging {
		data = cursorFeedContext(feed)
	} else if legacyStart {
		data = legacyFeedCursorContext(feed, req.PageSize)
	}
	data["show_header"] = true
	data["group_feed_header"] = feed.Type == "group"
	actor := CurrentUserUuid(c)
	data["show_profile_relations"] = actor != "" && feed.Type == "user"
	if actor != "" && feed.Type == "user" {
		profile, _ := s.CurrentUser(c)
		if actor == feed.Uuid || profile != nil && profile.IsSuper {
			data["feed_api_url"] = FeedApiKeyURL(feed.Id)
		}
	}
	if actor != "" && feed.Archive != nil {
		data["feed_archive"] = feed.Archive
		data["feed_archive_id"] = feed.Id
	}
	if actor != "" && feed.Type == "group" {
		ctx, cancel := DefaultTimeoutContext()
		// One authoritative Group view drives both presentation permissions:
		// members may post on the newest page, while admin/super viewers also
		// receive the settings entry point. ffdb still rechecks every post.
		view, viewErr := s.client.GetGroup(ctx, &pb.GetGroupRequest{
			GroupUuid: feed.Uuid, ViewerUuid: actor,
		})
		cancel()
		if viewErr == nil {
			data["group_members_url"] = "/groups/" + url.PathEscape(feed.Id) + "/members"
			profile, _ := s.CurrentUser(c)
			if req.Cursor == "" && req.Start == 0 &&
				(view.IsMember || view.IsAdmin || profile != nil && profile.IsSuper) {
				data["show_share"] = true
			}
			if canManageGroup(view, profile) {
				data["group_settings_url"] = "/groups/" + url.PathEscape(feed.Id) + "/settings"
				data["feed_api_url"] = FeedApiKeyURL(feed.Id)
			}
		}
	}
	s.renderFeed(c, data)
}

func (s *Server) PublicHandler(c *gin.Context) {
	req := &pb.FeedRequest{
		Id:         "public",
		PageSize:   30,
		ViewerUuid: CurrentUserUuid(c),
	}
	if redirectAnonymousLegacyFeedStart(c) {
		return
	}
	cursorPaging := configureFeedPagination(c.Request, req)
	legacyStart := !cursorPaging

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	// Public reads from TimelineIndex. A logged-in legacy page renders once,
	// then exposes only its cursor-based continuation.
	data := feedContext(feed, req.Start, req.PageSize)
	if cursorPaging {
		data = cursorFeedContext(feed)
	} else if legacyStart {
		data = legacyFeedCursorContext(feed, req.PageSize)
	}
	s.renderFeed(c, data)
}

func (s *Server) SearchHandler(c *gin.Context) {
	reqQuery := c.Request.URL.Query()
	query := reqQuery.Get("q")
	start := ParseStart(c.Request)
	req := &pb.SearchRequest{
		Query:      query,
		Start:      int32(start),
		PageSize:   30,
		ViewerUuid: CurrentUserUuid(c),
	}

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	data["query"] = query
	s.renderFeed(c, data)
}

func (s *Server) TagHandler(c *gin.Context) {
	name := c.Params.ByName("name")
	start := ParseStart(c.Request)
	req := &pb.SearchRequest{
		Query:      "#" + name,
		Start:      int32(start),
		PageSize:   30,
		ViewerUuid: CurrentUserUuid(c),
	}

	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	data := feedContext(feed, req.Start, req.PageSize)
	s.renderFeed(c, data)
}
