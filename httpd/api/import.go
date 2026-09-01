package api

import (
	"encoding/json"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type importSource struct {
	Kind      string `json:"kind"`
	AccountID string `json:"account_id"`
	ItemID    string `json:"item_id"`
	URL       string `json:"url"`
}

type importMetadata struct {
	Source      importSource `json:"source"`
	PublishedAt string       `json:"published_at"`
	Title       string       `json:"title"`
	BodyHTML    string       `json:"body_html"`
}

func decodeImportMetadata(raw string) (importMetadata, bool) {
	var metadata importMetadata
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return importMetadata{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return importMetadata{}, false
	}
	return metadata, true
}

func (h *Handler) importEntry(c *gin.Context) {
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
		if name != "metadata" {
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
	values := c.Request.MultipartForm.Value["metadata"]
	if len(values) != 1 || len(values[0]) > maxAPIBodyBytes+4096 {
		writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
		return
	}
	metadata, ok := decodeImportMetadata(values[0])
	if !ok || !utf8.ValidString(metadata.Title) || !utf8.ValidString(metadata.BodyHTML) ||
		len(metadata.Title) > maxAPITitleBytes || len(metadata.BodyHTML) > maxAPIBodyBytes {
		writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
		return
	}
	metadata.Title = strings.TrimSpace(bluemonday.StrictPolicy().Sanitize(metadata.Title))
	body, err := sanitizedAPIBody(metadata.BodyHTML)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
		return
	}
	files := c.Request.MultipartForm.File["file"]
	if metadata.Title == "" && strings.TrimSpace(body) == "" && len(files) == 0 {
		writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
		return
	}
	thumbnails, attachments, uploadErr := h.promoteUploads(files)
	if uploadErr != nil {
		failure := uploadErr.(*uploadFailure)
		writeError(c, failure.status, failure.code, failure.message)
		return
	}
	ctx, ok := h.trustedContext(c)
	if !ok {
		writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
		return
	}
	response, err := h.client.ImportFeedEntry(ctx, &pb.ImportFeedEntryRequest{
		SourceKind: metadata.Source.Kind, SourceAccountId: metadata.Source.AccountID,
		SourceItemId: metadata.Source.ItemID, SourceUrl: metadata.Source.URL,
		PublishedAt: metadata.PublishedAt, Title: metadata.Title, BodyHtml: body,
		Thumbnails: thumbnails, Files: attachments,
	})
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			writeError(c, http.StatusConflict, "source_identity_conflict", "Source identity conflicts with existing content")
			return
		}
		writeRPCError(c, err, false)
		return
	}
	if response == nil || response.Entry == nil || response.Entry.From == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
		return
	}
	httpStatus := http.StatusOK
	if response.Created {
		httpStatus = http.StatusCreated
	}
	c.JSON(httpStatus, gin.H{
		"created": response.Created,
		"data":    entryResponseDTO(response.Entry, response.Entry.From),
	})
}
