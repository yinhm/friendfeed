package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/protobuf/proto"
)

const maxUploadRequestBytes = media.MaxUploadFileBytes + 64<<10

var errInvalidUploadSourceURL = errors.New("invalid upload source URL")

func (s *Server) FetchEntry(c *gin.Context, uuid string) (*pb.Feed, error) {
	req := &pb.EntryRequest{
		Uuid:       uuid,
		ViewerUuid: CurrentUserUuid(c),
	}

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
	runes := []rune(title)
	if len(runes) > 42 {
		title = string(runes[:42])
	}
	return title
}

func (s *Server) EntryHandler(c *gin.Context) {
	uuid := c.Params.ByName("uuid")
	req := &pb.EntryRequest{
		Uuid:       uuid,
		ViewerUuid: CurrentUserUuid(c),
	}
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
		Id            string   `form:"id"`
		FeedUuid      string   `form:"feedUuid"`
		Body          string   `form:"body" binding:"required"`
		RawBody       string   `form:"rawBody"`
		FilesPresent  string   `form:"filesPresent"`
		ExistingFiles []string `form:"existingFile"`
		FileTokens    []string `form:"fileToken"`
		Assets        string   `form:"assets"`
		ResponseMode  string   `form:"responseMode"`
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
	assetTokens, err := decodeAssetTokens(form.Assets)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	images, filesFromAssets, err := partitionAssetTokens(s.secretKey, profile.Uuid, assetTokens, dt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired asset token"})
		return
	}
	filesFromAssets = append(filesFromAssets, form.FileTokens...)
	files, err := s.filesForEntryPost(profile.Uuid, entry, form.FilesPresent != "", form.ExistingFiles, filesFromAssets, dt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	entry.Files = files

	rawBody := form.RawBody
	if rawBody == "" {
		rawBody = form.Body
	}
	body := util.EntityToLink(util.DefaultSanitize(form.Body))
	rawBody, body, thumbnails, err := s.promoteEntryImages(entry.RawBody, rawBody, body, entry.Thumbnails, images)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	entry.RawBody = rawBody
	entry.Body = body
	entry.Thumbnails = thumbnails

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
	entry, err = s.client.PostEntry(ctx, entry)
	if RequestError(c, err) {
		fmt.Println(err)
		return
	}

	// rebuild entry graph...
	graph, err := s.CurrentGraph(c)
	if err != nil {
		return
	}
	c.JSON(http.StatusOK, postedEntryView(entry, profile, graph, form.ResponseMode))
	// c.Redirect(http.StatusFound, "/")
}

// postedEntryView applies the same presentation rules as an authoritative
// Feed read without mutating the RPC result. The browser chooses only whether
// it will render the response in a list or on a permalink; persistence and
// authorization never depend on this hint. Unknown modes safely use list
// semantics because that is the compact response used by new posts.
func postedEntryView(entry *pb.Entry, profile *pb.Profile, graph *pb.Graph, responseMode string) entryView {
	prepared := proto.Clone(entry).(*pb.Entry)
	prepareFeedEntry(prepared, profile, graph, responseMode != "permalink")
	return entryViewFromProto(prepared)
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
		Uuid:       entryId,
		User:       uuid,
		ViewerUuid: uuid,
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
	done, ok := s.beginUpload(CurrentUserUuid(c), true)
	if !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many uploads"})
		return
	}
	defer done()
	file, fileErr := c.FormFile("file")
	sourceURL := strings.TrimSpace(c.PostForm("sourceUrl"))
	if fileErr == nil && sourceURL != "" || fileErr != nil && sourceURL == "" {
		var maxBytesError *http.MaxBytesError
		if errors.As(fileErr, &maxBytesError) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "uploaded file is too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "provide exactly one of file or sourceUrl"})
		return
	}
	var content []byte
	var err error
	if fileErr == nil {
		src, openErr := file.Open()
		if openErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "can not read file"})
			return
		}
		defer src.Close()
		content, err = io.ReadAll(io.LimitReader(src, media.MaxUploadFileBytes+1))
	} else {
		content, err = s.fetchUploadedImage(sourceURL)
	}
	if err != nil {
		if errors.Is(err, errInvalidUploadSourceURL) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid source URL"})
		} else if errors.Is(err, media.ErrUploadFetchTimeout) {
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "source image timed out"})
		} else {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "image could not be read"})
		}
		return
	}
	if len(content) > media.MaxUploadFileBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "uploaded file is too large"})
		return
	}
	prepared, err := s.uploads.StageImage(content, 1024)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "unsupported or invalid image"})
		return
	}
	s.writeUploadedImage(c, prepared)
}

func (s *Server) fetchUploadedImage(source string) ([]byte, error) {
	if len(source) == 0 || len(source) > 2048 {
		return nil, errInvalidUploadSourceURL
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.User != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errInvalidUploadSourceURL
	}
	if s.uploadFetch == nil {
		s.uploadFetch = media.FetchUploadedImage
	}
	return s.uploadFetch(parsed.String())
}

func (s *Server) writeUploadedImage(c *gin.Context, prepared *media.StagedImage) {
	if prepared == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "can not process image"})
		return
	}
	objects := make([]stagedObject, 0, len(prepared.Objects))
	for _, object := range prepared.Objects {
		objects = append(objects, stagedObject{Name: object.Name, Digest: object.Digest, Extension: object.Extension, MimeType: object.MimeType, Size: object.Size, Role: object.Role})
	}
	token, err := signAssetToken(s.secretKey, assetTokenPayload{Version: 1, Actor: CurrentUserUuid(c), Kind: "image", Width: prepared.ThumbnailWidth, Height: prepared.ThumbnailHeight, Expires: time.Now().UTC().Add(assetTokenLifetime).Unix(), Objects: objects})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "can not create asset token"})
		return
	}
	original, thumbnail := prepared.Objects[0], prepared.Objects[0]
	for _, object := range prepared.Objects {
		if object.Role == "original" {
			original = object
		}
		if object.Role == "thumbnail" {
			thumbnail = object
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"assetToken":  token,
		"url":         s.uploads.StagingURL(thumbnail),
		"originalUrl": s.uploads.StagingURL(original),
		"width":       prepared.ThumbnailWidth,
		"height":      prepared.ThumbnailHeight,
		"mimeType":    original.MimeType,
		"size":        original.Size,
	})
}
