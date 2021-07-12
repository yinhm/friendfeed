package server

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/gofrs/uuid"
	pb "github.com/yinhm/friendfeed/proto"
	"github.com/yinhm/friendfeed/util"
)

func (s *Server) FetchEntry(c *gin.Context, uuid string) (*pb.Feed, error) {
	req := &pb.EntryRequest{Uuid: uuid}

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	// FIME: why FetchEntry return feed, I forgot...
	feed, err := s.client.FetchEntry(ctx, req)
	if RequestError(c, err) {
		return nil, err
	}
	return feed, nil
}

// TODO: allow cross post to multiply feeds
func (s *Server) EntryPostHandler(c *gin.Context) {
	var form struct {
		Id     string `form:"id"`
		FeedId string `form:"feedid"`
		Body   string `form:"body" binding:"required"`
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

	if form.FeedId == "" { // new entry
		form.FeedId = profile.Id
		entry.Id = uuid1.String()
		entry.Date = dt.Format(time.RFC3339)
	} else {
		if form.Id != "" { // edit entry
			// only allow edit self entry for now
			feed, err := s.FetchEntry(c, form.Id)
			if err != nil {
				return
			}
			// restore old entry, Id, date etc...
			entry = feed.Entries[0]
			// rewrite feedid when edit entry
			form.FeedId = feed.Id
		}
		if !s.feedWritable(c, strings.ToLower(form.FeedId)) {
			c.AbortWithStatus(401)
			return
		}
	}
	body := util.DefaultSanitize(form.Body)
	body = util.EntityToLink(body)

	ctx, cancel := DefaultTimeoutContext()
	defer cancel()

	from := &pb.Feed{
		Id:      profile.Id,
		Name:    profile.Name,
		Type:    profile.Type,
		Picture: profile.Picture,
	}

	entry.Body = body
	entry.RawBody = form.Body
	entry.From = from
	// To:      []*pb.Feed{from},
	// Thumbnails: thumbnails,
	entry.ProfileUuid = profile.Uuid

	entry, err := s.client.PostEntry(ctx, entry)
	log.Println(err)
	if RequestError(c, err) {
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
