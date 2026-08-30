package api

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/util"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	maxAPITitleBytes = 512
	maxAPIBodyBytes  = 256 << 10
)

var activeContentElements = map[string]bool{
	"img": true, "picture": true, "source": true, "video": true, "audio": true,
	"object": true, "embed": true, "iframe": true, "canvas": true,
}

func stripExternalMedia(raw string) (string, error) {
	contextNode := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(raw), contextNode)
	if err != nil {
		return "", err
	}
	var strip func(*html.Node)
	strip = func(parent *html.Node) {
		for node := parent.FirstChild; node != nil; {
			next := node.NextSibling
			if node.Type == html.ElementNode && activeContentElements[strings.ToLower(node.Data)] {
				parent.RemoveChild(node)
			} else {
				strip(node)
			}
			node = next
		}
	}
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	strip(root)
	var output bytes.Buffer
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&output, child); err != nil {
			return "", err
		}
	}
	return output.String(), nil
}

func sanitizedAPIBody(raw string) (string, error) {
	withoutMedia, err := stripExternalMedia(raw)
	if err != nil {
		return "", err
	}
	return util.EntityToLink(util.DefaultSanitize(withoutMedia)), nil
}

func (h *Handler) postEntry(c *gin.Context) {
	if h.uploads == nil {
		writeError(c, http.StatusServiceUnavailable, "unavailable", "Service unavailable")
		return
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
		return
	}
	if err := c.Request.ParseMultipartForm(media.MaxUploadFileBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "Payload too large")
		} else {
			writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
		}
		return
	}
	if c.Request.MultipartForm == nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	for name := range c.Request.MultipartForm.Value {
		if name != "title" && name != "body_html" {
			writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
			return
		}
	}
	for name := range c.Request.MultipartForm.File {
		if name != "file" {
			writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
			return
		}
	}
	if len(c.Request.MultipartForm.Value["title"]) > 1 || len(c.Request.MultipartForm.Value["body_html"]) > 1 {
		writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
		return
	}
	title := strings.TrimSpace(c.PostForm("title"))
	bodyRaw := c.PostForm("body_html")
	if !utf8.ValidString(title) || !utf8.ValidString(bodyRaw) || len(title) > maxAPITitleBytes || len(bodyRaw) > maxAPIBodyBytes {
		writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
		return
	}
	title = strings.TrimSpace(bluemonday.StrictPolicy().Sanitize(title))
	body, err := sanitizedAPIBody(bodyRaw)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
		return
	}
	files := c.Request.MultipartForm.File["file"]
	if len(files) > media.MaxEntryAttachments || title == "" && strings.TrimSpace(body) == "" && len(files) == 0 {
		writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
		return
	}

	entry := &pb.Entry{Title: title, Body: body}
	totalBytes := int64(0)
	for _, header := range files {
		if header == nil || header.Size > media.MaxUploadFileBytes {
			writeError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "Payload too large")
			return
		}
		source, err := header.Open()
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
			return
		}
		content, readErr := io.ReadAll(io.LimitReader(source, media.MaxUploadFileBytes+1))
		closeErr := source.Close()
		if readErr != nil || closeErr != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
			return
		}
		if len(content) > media.MaxUploadFileBytes {
			writeError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "Payload too large")
			return
		}
		totalBytes += int64(len(content))
		if totalBytes > media.MaxEntryAttachmentBytes {
			writeError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "Payload too large")
			return
		}

		if stagedImage, imageErr := h.uploads.StageImage(content, 1024); imageErr == nil {
			published, promoteErr := h.uploads.PromoteImage(stagedImage)
			if promoteErr != nil {
				writeError(c, http.StatusInternalServerError, "internal_error", "Unable to publish media")
				return
			}
			entry.Thumbnails = append(entry.Thumbnails, &pb.Thumbnail{
				Url: published.ThumbnailURL, Link: published.URL,
				Width: int32(published.Width), Height: int32(published.Height),
			})
			continue
		}
		stagedFile, fileErr := h.uploads.StageAttachment(header.Filename, content)
		if fileErr != nil {
			writeError(c, http.StatusUnsupportedMediaType, "unsupported_media", "Media type is unsupported")
			return
		}
		published, promoteErr := h.uploads.PromoteAttachment(stagedFile)
		if promoteErr != nil {
			writeError(c, http.StatusInternalServerError, "internal_error", "Unable to publish media")
			return
		}
		entry.Files = append(entry.Files, &pb.File{
			Url: published.URL, Name: published.Name, Type: published.MimeType, Size: int32(published.Size),
		})
	}

	ctx, ok := h.trustedContext(c)
	if !ok {
		writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
		return
	}
	created, err := h.client.PostEntry(ctx, entry)
	if err != nil {
		writeRPCError(c, err, false)
		return
	}
	if created == nil || created.From == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": entryResponseDTO(created, created.From)})
}
