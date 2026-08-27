package server

import (
	"errors"
	"fmt"
	"io"
	"net"
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
	"golang.org/x/exp/utf8string"
)

const maxUploadRequestBytes = media.MaxUploadFileBytes + 64<<10

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
	titleUtf8 := utf8string.NewString(title)
	if titleUtf8.RuneCount() > 42 {
		title = titleUtf8.Slice(0, 42)
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
	rawBody, body, thumbnails, err := s.promoteEntryImages(rawBody, body, entry.Thumbnails, images)
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
	select {
	case s.uploadRequests <- struct{}{}:
		defer func() { <-s.uploadRequests }()
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many uploads"})
		return
	}
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
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
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
	select {
	case s.imageOperations <- struct{}{}:
		defer func() { <-s.imageOperations }()
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "image processor is busy"})
		return
	}
	prepared, err := media.PrepareUploadedImage(content, 1024)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "unsupported or invalid image"})
		return
	}
	s.writeUploadedImage(c, prepared)
}

func (s *Server) fetchUploadedImage(source string) ([]byte, error) {
	if len(source) == 0 || len(source) > 2048 {
		return nil, errors.New("invalid source URL")
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.User != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid source URL")
	}
	obj := &media.Object{Url: parsed.String()}
	if _, err := s.media.Fetch(obj); err != nil {
		return nil, err
	}
	if len(obj.Content) > media.MaxUploadFileBytes {
		return nil, errors.New("source image is too large")
	}
	return obj.Content, nil
}

func (s *Server) writeUploadedImage(c *gin.Context, prepared *media.PreparedImage) {
	if prepared == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "can not process image"})
		return
	}
	originalName, originalDigest, err := s.staging.Put(prepared.Original, prepared.Extension)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "can not stage image"})
		return
	}
	thumbName, thumbDigest, thumbExt := originalName, originalDigest, prepared.Extension
	thumbMime := prepared.MimeType
	if string(prepared.Original) != string(prepared.Thumbnail) {
		thumbExt, thumbMime = "jpg", "image/jpeg"
		thumbName, thumbDigest, err = s.staging.Put(prepared.Thumbnail, thumbExt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "can not stage image thumbnail"})
			return
		}
	}
	objects := []stagedObject{{Name: originalName, Digest: originalDigest, Extension: prepared.Extension, MimeType: prepared.MimeType, Size: len(prepared.Original), Role: "original"}}
	if thumbName != originalName {
		objects = append(objects, stagedObject{Name: thumbName, Digest: thumbDigest, Extension: thumbExt, MimeType: thumbMime, Size: len(prepared.Thumbnail), Role: "thumbnail"})
	}
	token, err := signAssetToken(s.secretKey, assetTokenPayload{Version: 1, Actor: CurrentUserUuid(c), Kind: "image", Width: prepared.ThumbnailWidth, Height: prepared.ThumbnailHeight, Expires: time.Now().UTC().Add(assetTokenLifetime).Unix(), Objects: objects})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "can not create asset token"})
		return
	}
	base := strings.TrimRight(s.mediaBaseURL, "/") + "/" + media.StagingDirectory + "/"
	c.JSON(http.StatusOK, gin.H{
		"assetToken":  token,
		"url":         base + thumbName,
		"originalUrl": base + originalName,
		"width":       prepared.ThumbnailWidth,
		"height":      prepared.ThumbnailHeight,
		"mimeType":    prepared.MimeType,
		"size":        len(prepared.Original),
	})
}
