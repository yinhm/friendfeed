package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/internal/feedprincipal"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type feedDTO struct {
	ID          string `json:"id"`
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PictureURL  string `json:"picture_url"`
	Type        string `json:"type"`
	Private     bool   `json:"private"`
}

type entryActorDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PictureURL string `json:"picture_url,omitempty"`
}

type entryFeedDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type entryViaDTO struct {
	Name string `json:"name"`
}

type imageDTO struct {
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url"`
	Width        int32  `json:"width"`
	Height       int32  `json:"height"`
}

type fileDTO struct {
	URL      string `json:"url"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Size     int32  `json:"size"`
}

type entryDTO struct {
	ID        string        `json:"id"`
	Title     string        `json:"title,omitempty"`
	BodyHTML  string        `json:"body_html"`
	CreatedAt string        `json:"created_at"`
	Author    entryActorDTO `json:"author"`
	Feed      entryFeedDTO  `json:"feed"`
	Via       entryViaDTO   `json:"via"`
	Images    []imageDTO    `json:"images"`
	Files     []fileDTO     `json:"files"`
}

type paginationDTO struct {
	NextCursor string `json:"next_cursor"`
}

func feedResponseDTO(feed *pb.Feed) feedDTO {
	return feedDTO{ID: feed.Id, UUID: feed.Uuid, Name: feed.Name,
		Description: feed.Description, PictureURL: feed.Picture,
		Type: feed.Type, Private: feed.Private}
}

func entryResponseDTO(entry *pb.Entry, target *pb.Feed) entryDTO {
	result := entryDTO{
		ID: entry.Id, Title: entry.Title, BodyHTML: entry.Body, CreatedAt: entry.Date,
		Images: make([]imageDTO, 0, len(entry.Thumbnails)), Files: make([]fileDTO, 0, len(entry.Files)),
	}
	if entry.From != nil {
		result.Author = entryActorDTO{ID: entry.From.Id, Name: entry.From.Name, PictureURL: entry.From.Picture}
	}
	if target != nil {
		result.Feed = entryFeedDTO{ID: target.Id, Name: target.Name}
	}
	if entry.Via != nil {
		result.Via = entryViaDTO{Name: entry.Via.Name}
	}
	for _, thumbnail := range entry.Thumbnails {
		if thumbnail == nil {
			continue
		}
		original := thumbnail.Link
		if original == "" {
			original = thumbnail.Url
		}
		result.Images = append(result.Images, imageDTO{
			URL: original, ThumbnailURL: thumbnail.Url, Width: thumbnail.Width, Height: thumbnail.Height,
		})
	}
	for _, file := range entry.Files {
		if file == nil {
			continue
		}
		result.Files = append(result.Files, fileDTO{
			URL: file.Url, Name: file.Name, MimeType: file.Type, Size: file.Size,
		})
	}
	return result
}

func (h *Handler) trustedContext(c *gin.Context) (context.Context, bool) {
	p := principal(c)
	return feedprincipal.WithOutgoing(c.Request.Context(), p.FeedUUID, p.KeyID)
}

func (h *Handler) fetchFeed(c *gin.Context, limit int32, cursor string) (*pb.Feed, bool) {
	ctx, ok := h.trustedContext(c)
	if !ok {
		writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
		return nil, false
	}
	feed, err := h.client.FetchFeed(ctx, &pb.FeedRequest{
		ProfileUuid: principal(c).FeedUUID, CursorPaging: true, PageSize: limit, Cursor: cursor,
	})
	if err != nil {
		writeRPCError(c, err, false)
		return nil, false
	}
	return feed, true
}

func (h *Handler) getFeed(c *gin.Context) {
	feed, ok := h.fetchFeed(c, 1, "")
	if ok {
		c.JSON(http.StatusOK, gin.H{"data": feedResponseDTO(feed)})
	}
}

func parsePagination(c *gin.Context) (int32, string, bool) {
	for name := range c.Request.URL.Query() {
		if name != "limit" && name != "cursor" {
			writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
			return 0, "", false
		}
	}
	limit := int32(50)
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
			return 0, "", false
		}
		limit = int32(parsed)
	}
	return limit, c.Query("cursor"), true
}

func (h *Handler) listEntries(c *gin.Context) {
	limit, cursor, ok := parsePagination(c)
	if !ok {
		return
	}
	feed, ok := h.fetchFeed(c, limit, cursor)
	if !ok {
		return
	}
	entries := make([]entryDTO, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		if entry != nil {
			entries = append(entries, entryResponseDTO(entry, feed))
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": entries, "pagination": paginationDTO{NextCursor: feed.NextCursor}})
}

func (h *Handler) getEntry(c *gin.Context) {
	ctx, ok := h.trustedContext(c)
	if !ok {
		writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
		return
	}
	feed, err := h.client.FetchEntry(ctx, &pb.EntryRequest{Uuid: c.Param("entry_id")})
	if err != nil {
		writeRPCError(c, err, true)
		return
	}
	if feed == nil || len(feed.Entries) != 1 || feed.Entries[0] == nil {
		writeError(c, http.StatusNotFound, "not_found", "Entry not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": entryResponseDTO(feed.Entries[0], feed)})
}

func writeRPCError(c *gin.Context, err error, entry bool) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		writeError(c, http.StatusBadRequest, "invalid_request", "Request is invalid")
	case codes.NotFound:
		message := "Feed not found"
		if entry {
			message = "Entry not found"
		}
		writeError(c, http.StatusNotFound, "not_found", message)
	case codes.PermissionDenied:
		if entry {
			writeError(c, http.StatusNotFound, "not_found", "Entry not found")
		} else {
			writeError(c, http.StatusForbidden, "forbidden", "Feed is unavailable")
		}
	case codes.Unavailable, codes.DeadlineExceeded:
		writeError(c, http.StatusServiceUnavailable, "unavailable", "Service unavailable")
	default:
		writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
	}
}
