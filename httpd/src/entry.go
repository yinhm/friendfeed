package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
	"golang.org/x/exp/utf8string"
)

const maxUploadRequestBytes = 20 << 20

func (s *Server) FetchEntry(c *gin.Context, uuid string) (*pb.Feed, error) {
	req := &pb.EntryRequest{Uuid: uuid}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	// FIME: forgot why FetchEntry return feed...
	feed, err := s.client.FetchEntry(ctx, req)
	if err == nil {
		_, err = firstEntry(feed)
	}
	if RequestError(c, err) {
		return nil, err
	}
	return feed, nil
}

// entryTitle derives the plain-text page title for the entry show page:
// any HTML markup is stripped from both the entry title and the body
// fallback, and the result is capped at 42 runes.
func entryTitle(entry *pb.Entry) string {
	title := entry.Title
	if title == "" {
		title = entry.Body
	}
	title = titleSanitizer.Sanitize(title)
	titleUtf8 := utf8string.NewString(title)
	if titleUtf8.RuneCount() > 42 {
		title = titleUtf8.Slice(0, 42)
	}
	return title
}

func (s *Server) EntryHandler(c *gin.Context) {
	uuid := c.Params.ByName("uuid")
	req := &pb.EntryRequest{Uuid: uuid}
	_, feed, err := s.FetchFeed(c, req)
	if RequestError(c, err) {
		return
	}

	entry := feed.Entries[0]
	title := entryTitle(entry)
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
	entry := &pb.Entry{}

	if form.Id == "" { // new entry
		entryUUID, err := uuid.NewV4()
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		entry.Id = entryUUID.String()
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

func (s *Server) UploadHandler(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadRequestBytes)

	// eid := c.PostForm("eid")
	file, err := c.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			c.String(http.StatusRequestEntityTooLarge, "uploaded file is too large")
			return
		}
		c.String(http.StatusBadRequest, fmt.Sprintf("error on formdata: %s", err.Error()))
		return
	}

	src, err := file.Open()
	if err != nil {
		c.String(http.StatusBadRequest, fmt.Sprintf("can not read file: %s", err.Error()))
		return
	}
	defer src.Close()

	content, err := io.ReadAll(src)
	if err != nil {
		c.String(http.StatusBadRequest, fmt.Sprintf("can not read file: %s", err.Error()))
		return
	}

	fileUUID := model.UniqueKeyFrom("web", "upload", file.Filename, randhash())
	filename := fmt.Sprintf("%x", fileUUID)

	obj := &media.Object{
		Filename: filename,
		MimeType: file.Header.Get("Content-Type"),
		Content:  content,
	}

	if _, err = s.media.Post(obj); err != nil {
		c.String(http.StatusInternalServerError, "can not write file")
		return
	}
	thumbObj, err := s.media.Thumbnail(obj)
	if err != nil {
		c.String(http.StatusInternalServerError, "can not write file")
		return
	}

	ret := gin.H{
		"url":      filepath.Join("/file", obj.Path),
		"thumbUrl": filepath.Join("/file", thumbObj.Path),
	}
	c.JSON(200, ret)
}
