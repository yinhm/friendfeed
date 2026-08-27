package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
)

const (
	attachmentTokenLifetime = 24 * time.Hour
	maxEntryAttachments     = 10
	maxEntryAttachmentBytes = 100 << 20
)

type attachmentTokenPayload struct {
	Actor    string `json:"actor"`
	Key      string `json:"key"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Size     int    `json:"size"`
	Expires  int64  `json:"expires"`
}

func signAttachmentToken(secret string, payload attachmentTokenPayload) (string, error) {
	if secret == "" {
		return "", errors.New("attachment token secret is not configured")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func verifyAttachmentToken(secret, token, actor string, now time.Time) (*attachmentTokenPayload, error) {
	body, signature, ok := strings.Cut(token, ".")
	if !ok || secret == "" {
		return nil, errors.New("invalid attachment token")
	}
	want, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return nil, errors.New("invalid attachment token")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	if !hmac.Equal(want, mac.Sum(nil)) {
		return nil, errors.New("invalid attachment token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, errors.New("invalid attachment token")
	}
	var payload attachmentTokenPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("invalid attachment token")
	}
	if payload.Actor == "" || payload.Actor != actor || payload.Key == "" || payload.Name == "" ||
		payload.Size <= 0 || payload.Size > media.MaxUploadFileBytes || now.Unix() > payload.Expires {
		return nil, errors.New("invalid or expired attachment token")
	}
	if _, err := digestFromObjectKey(payload.Key); err != nil {
		return nil, errors.New("invalid attachment token")
	}
	return &payload, nil
}

func digestFromObjectKey(key string) (string, error) {
	digest := strings.ReplaceAll(key, "/", "")
	if len(digest) != sha256.Size*2 {
		return "", errors.New("invalid attachment object key")
	}
	if _, err := hex.DecodeString(digest); err != nil || key != digest[:1]+"/"+digest[1:2]+"/"+digest[2:] {
		return "", errors.New("invalid attachment object key")
	}
	return digest, nil
}

func attachmentObjectKey(digest string) (string, error) {
	if len(digest) != sha256.Size*2 {
		return "", errors.New("invalid attachment digest")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", errors.New("invalid attachment digest")
	}
	return digest[:1] + "/" + digest[1:2] + "/" + digest[2:], nil
}

func attachmentDownloadURL(entryID, digest, name string) string {
	return "/e/" + url.PathEscape(entryID) + "/files/" + digest + "/" + url.PathEscape(name)
}

func (s *Server) UploadFileHandler(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadRequestBytes)
	actor := CurrentUserUuid(c)
	file, err := c.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "uploaded file is too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	source, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "can not read file"})
		return
	}
	defer source.Close()
	content, err := io.ReadAll(io.LimitReader(source, media.MaxUploadFileBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "can not read file"})
		return
	}
	if len(content) > media.MaxUploadFileBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "uploaded file is too large"})
		return
	}
	info, err := media.InspectAttachment(file.Filename, content)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported or invalid attachment"})
		return
	}
	obj := &media.Object{Content: content, MimeType: info.MimeType}
	if _, err := s.media.Post(obj); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "can not store attachment"})
		return
	}
	token, err := signAttachmentToken(s.secretKey, attachmentTokenPayload{
		Actor: actor, Key: obj.Path, Name: info.Name, MimeType: info.MimeType,
		Size: info.Size, Expires: time.Now().UTC().Add(attachmentTokenLifetime).Unix(),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "can not create attachment token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"assetToken": token, "name": info.Name, "mimeType": info.MimeType, "size": info.Size,
	})
}

func (s *Server) filesForEntryPost(actor string, entry *pb.Entry, aware bool, keepURLs, tokens []string, now time.Time) ([]*pb.File, error) {
	files := entry.GetFiles()
	if aware {
		keep := make(map[string]bool, len(keepURLs))
		for _, raw := range keepURLs {
			keep[raw] = true
		}
		retained := make([]*pb.File, 0, len(files)+len(tokens))
		for _, file := range files {
			if file != nil && keep[file.Url] {
				retained = append(retained, file)
			}
		}
		files = retained
	}
	seen := make(map[string]bool, len(files)+len(tokens))
	for _, file := range files {
		if file != nil {
			seen[file.Url] = true
		}
	}
	for _, token := range tokens {
		payload, err := verifyAttachmentToken(s.secretKey, token, actor, now)
		if err != nil {
			return nil, err
		}
		digest, _ := digestFromObjectKey(payload.Key)
		fileURL := attachmentDownloadURL(entry.Id, digest, payload.Name)
		if seen[fileURL] {
			continue
		}
		seen[fileURL] = true
		files = append(files, &pb.File{
			Url: fileURL, Type: payload.MimeType, Name: payload.Name, Size: int32(payload.Size),
		})
	}
	if len(files) > maxEntryAttachments {
		return nil, fmt.Errorf("an entry may contain at most %d attachments", maxEntryAttachments)
	}
	total := int64(0)
	for _, file := range files {
		if file != nil && file.Size > 0 {
			total += int64(file.Size)
		}
	}
	if total > maxEntryAttachmentBytes {
		return nil, fmt.Errorf("entry attachments exceed %d byte limit", maxEntryAttachmentBytes)
	}
	return files, nil
}

func (s *Server) DownloadFileHandler(c *gin.Context) {
	entryID := c.Param("uuid")
	digest := strings.ToLower(c.Param("digest"))
	name := path.Base(c.Param("name"))
	key, err := attachmentObjectKey(digest)
	if err != nil || name == "." || name == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	feed, err := s.FetchEntry(c, entryID)
	if err != nil {
		return
	}
	entry, err := firstEntry(feed)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	wantURL := attachmentDownloadURL(entryID, digest, name)
	found := false
	for _, file := range entry.Files {
		if file != nil && file.Url == wantURL && file.Name == name {
			found = true
			break
		}
	}
	if !found || s.localMedia == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	object, err := s.localMedia.OpenObject(key)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	defer object.Close()
	info, err := object.Stat()
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	c.Header("Content-Length", strconv.FormatInt(info.Size(), 10))
	http.ServeContent(c.Writer, c.Request, name, info.ModTime(), object)
}
