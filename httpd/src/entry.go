package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
	"golang.org/x/exp/utf8string"
)

func (s *Server) FetchEntry(c *gin.Context, uuid string) (*pb.Feed, error) {
	req := &pb.EntryRequest{Uuid: uuid}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	// FIME: forgot why FetchEntry return feed...
	feed, err := s.client.FetchEntry(ctx, req)
	if RequestError(c, err) {
		return nil, err
	}
	return feed, nil
}

func (s *Server) EntryHandler(c *gin.Context) {
	uuid := c.Params.ByName("uuid")
	req := &pb.EntryRequest{Uuid: uuid}
	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	entry := feed.Entries[0]
	title := entry.Title
	if title == "" {
		title = htmlSanitizer.Sanitize(entry.Body)
		titleUtf8 := utf8string.NewString(title)
		if titleUtf8.RuneCount() > 42 {
			title = titleUtf8.Slice(0, 42)
		}
	}
	data := pongo2.Context{
		"title":       title,
		"name":        entry.From.Name,
		"feed":        feed,
		"show_header": false,
		"show_share":  false,
		"show_paging": false,
		"onpage":      true,
		"onpage_edit": false,
	}
	s.renderFeed(c, data)
	// s.HTML(c, 200, "feed.html", data)
}

// TODO: allow cross post to multiply feeds
func (s *Server) EntryPostHandler(c *gin.Context) {
	var form struct {
		Id       string `form:"id"`
		FeedUuid string `form:"feedUuid"`
		Body     string `form:"body" binding:"required"`
		RawBody  string `form:"rawBody"`
	}
	if err := c.MustBindWith(&form, binding.FormMultipart); err != nil {
		return
	}

	// init profile and new entry
	profile, _ := s.CurrentUser(c)
	dt := time.Now().UTC()
	name := profile.Uuid + "/" + dt.Format(time.RFC3339)
	uuid1 := uuid.NewV5(uuid.NamespaceURL, name)
	entry := &pb.Entry{}

	if form.Id == "" { // new entry
		entry.Id = uuid1.String()
		entry.Date = dt.Format(time.RFC3339)
	} else { // edit entry
		// only allow edit self entry for now
		feed, err := s.FetchEntry(c, form.Id)
		if err != nil {
			return
		}
		// restore old entry, Id, date etc...
		entry = feed.Entries[0]
		// rewrite feedid when edit entry
		form.FeedUuid = feed.Uuid
	}

	if !s.feedWritable(c, strings.ToLower(form.FeedUuid)) {
		c.AbortWithStatus(401)
		return
	}

	if form.RawBody != "" {
		entry.RawBody = form.RawBody
	} else {
		entry.RawBody = form.Body
	}
	entry.Body = util.EntityToLink(util.DefaultSanitize(form.Body))

	from := &pb.Feed{
		Id:      profile.Id,
		Name:    profile.Name,
		Type:    profile.Type,
		Picture: profile.Picture,
	}
	entry.From = from
	// To:      []*pb.Feed{from},
	// Thumbnails: thumbnails,
	entry.ProfileUuid = profile.Uuid // 发布人
	entry.FeedUuid = form.FeedUuid   // 写到具体的 Feed

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()
	entry, err := s.client.PostEntry(ctx, entry)
	if RequestError(c, err) {
		fmt.Println(err)
		return
	}

	// rebuild entry graph...
	graph, err := s.CurrentGraph(c)
	if err != nil {
		return
	}
	entry.RebuildCommand(profile, graph)
	basetime, _ := time.Parse(time.RFC3339, entry.Date)
	entry.Date = util.FormatTime(basetime)
	// if format {
	// 	e.FormatComments(int32(0))
	// 	e.FormatLikes(int32(0))
	// }
	entry.RebuildCommentsCommand(profile, graph)
	c.JSON(200, entry)
	// c.Redirect(http.StatusFound, "/")
}

func (s *Server) EntryDeleteHandler(c *gin.Context) {
	c.Request.ParseForm()
	entryId := c.Request.Form.Get("entry")
	if entryId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "bad request"})
		return
	}

	uuid := CurrentUserUuid(c)
	req := &pb.EntryRequest{
		Uuid: entryId,
		User: uuid,
	}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	_, err := s.client.DeleteEntry(ctx, req)
	if RequestError(c, err) {
		return
	}
	c.JSON(200, entryId)
}
