package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
)

const (
	assetTokenLifetime      = 24 * time.Hour
	maxEntryAttachments     = 10
	maxEntryAttachmentBytes = 100 << 20
)

type stagedObject struct {
	Name      string `json:"name"`
	Digest    string `json:"digest"`
	Extension string `json:"extension"`
	MimeType  string `json:"mime_type"`
	Size      int    `json:"size"`
	Role      string `json:"role"`
}

type assetTokenPayload struct {
	Version int            `json:"v"`
	Actor   string         `json:"actor"`
	Kind    string         `json:"kind"`
	Name    string         `json:"display_name,omitempty"`
	Width   int            `json:"width,omitempty"`
	Height  int            `json:"height,omitempty"`
	Expires int64          `json:"expires"`
	Objects []stagedObject `json:"objects"`
}

func signAssetToken(secret string, payload assetTokenPayload) (string, error) {
	if secret == "" {
		return "", errors.New("asset token secret is not configured")
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

func verifyAssetToken(secret, token, actor string, now time.Time) (*assetTokenPayload, error) {
	body, signature, ok := strings.Cut(token, ".")
	if !ok || secret == "" {
		return nil, errors.New("invalid asset token")
	}
	want, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return nil, errors.New("invalid asset token")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	if !hmac.Equal(want, mac.Sum(nil)) {
		return nil, errors.New("invalid asset token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, errors.New("invalid asset token")
	}
	var payload assetTokenPayload
	if json.Unmarshal(raw, &payload) != nil || payload.Version != 1 || payload.Actor != actor ||
		(payload.Kind != "image" && payload.Kind != "file") || len(payload.Objects) == 0 || now.Unix() > payload.Expires {
		return nil, errors.New("invalid or expired asset token")
	}
	for _, object := range payload.Objects {
		if object.Name == "" || object.Digest == "" || object.Extension == "" || object.Size <= 0 || object.Size > media.MaxUploadFileBytes {
			return nil, errors.New("invalid asset token")
		}
	}
	return &payload, nil
}

func (s *Server) UploadFileHandler(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadRequestBytes)
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
	if err != nil || strings.HasPrefix(info.MimeType, "image/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported or invalid attachment"})
		return
	}
	name, digest, err := s.staging.Put(content, info.Extension)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "can not stage attachment"})
		return
	}
	token, err := signAssetToken(s.secretKey, assetTokenPayload{
		Version: 1, Actor: CurrentUserUuid(c), Kind: "file", Name: info.Name,
		Expires: time.Now().UTC().Add(assetTokenLifetime).Unix(),
		Objects: []stagedObject{{Name: name, Digest: digest, Extension: info.Extension, MimeType: info.MimeType, Size: info.Size, Role: "file"}},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "can not create asset token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"assetToken": token, "name": info.Name, "mimeType": info.MimeType, "size": info.Size})
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
		payload, err := verifyAssetToken(s.secretKey, token, actor, now)
		if err != nil || payload.Kind != "file" || len(payload.Objects) != 1 {
			return nil, errors.New("invalid file asset token")
		}
		object := payload.Objects[0]
		key, err := s.staging.Promote(object.Name, object.Digest, object.Extension, object.Size)
		if err != nil {
			return nil, fmt.Errorf("promote attachment: %w", err)
		}
		fileURL := strings.TrimRight(s.mediaBaseURL, "/") + "/" + key + "?download=" + base64.RawURLEncoding.EncodeToString([]byte(payload.Name))
		if !seen[fileURL] {
			seen[fileURL] = true
			files = append(files, &pb.File{Url: fileURL, Type: object.MimeType, Name: payload.Name, Size: int32(object.Size)})
		}
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
