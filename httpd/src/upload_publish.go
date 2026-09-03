package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
)

func decodeAssetTokens(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var tokens []string
	if err := json.Unmarshal([]byte(raw), &tokens); err != nil || len(tokens) > 30 {
		return nil, errors.New("invalid assets")
	}
	return tokens, nil
}

func partitionAssetTokens(secret, actor string, tokens []string, now time.Time) (map[string]*assetTokenPayload, []string, error) {
	images := make(map[string]*assetTokenPayload)
	files := make([]string, 0, len(tokens))
	for _, token := range tokens {
		payload, err := verifyAssetToken(secret, token, actor, now)
		if err != nil {
			return nil, nil, err
		}
		switch payload.Kind {
		case "file":
			files = append(files, token)
		case "image":
			images[token] = payload
		default:
			return nil, nil, errors.New("invalid entry asset token")
		}
	}
	return images, files, nil
}

func (s *Server) promoteAvatarToken(token, actor string) (string, error) {
	payload, err := verifyAssetToken(s.secretKey, token, actor, time.Now().UTC())
	if err != nil || payload.Kind != "avatar" || len(payload.Objects) != 1 || payload.Objects[0].Role != "avatar" {
		return "", errors.New("invalid avatar upload")
	}
	object := payload.Objects[0]
	published, err := s.uploads.PromoteImage(&media.StagedImage{
		Width: payload.Width, Height: payload.Height, ThumbnailWidth: payload.Width, ThumbnailHeight: payload.Height,
		Objects: []media.StagedObject{{Name: object.Name, Digest: object.Digest, Extension: object.Extension, MimeType: object.MimeType, Size: object.Size, Role: "avatar"}},
	})
	if err != nil {
		return "", fmt.Errorf("promote avatar: %w", err)
	}
	return published.URL, nil
}

func (s *Server) pictureFromAvatarForm(c *gin.Context, current, actor string) (string, error) {
	switch c.PostForm("picture_action") {
	case "", "keep":
		return current, nil
	case "default":
		return "", nil
	case "replace":
		return s.promoteAvatarToken(c.PostForm("picture_asset_token"), actor)
	default:
		return "", errors.New("invalid picture action")
	}
}

func plateImageURLs(rawBody string) map[string]bool {
	urls := make(map[string]bool)
	var nodes []any
	if json.Unmarshal([]byte(rawBody), &nodes) != nil {
		return urls
	}
	var visit func(any)
	visit = func(value any) {
		node, ok := value.(map[string]any)
		if !ok {
			return
		}
		if node["type"] == "img" {
			for _, field := range []string{"url", "originalUrl"} {
				if raw, ok := node[field].(string); ok {
					urls[raw] = true
				}
			}
		}
		if children, ok := node["children"].([]any); ok {
			for _, child := range children {
				visit(child)
			}
		}
	}
	for _, node := range nodes {
		visit(node)
	}
	return urls
}

func (s *Server) promoteEntryImages(oldRawBody, rawBody, body string, old []*pb.Thumbnail, assets map[string]*assetTokenPayload) (string, string, []*pb.Thumbnail, error) {
	if rawBody == "" {
		return rawBody, body, old, nil
	}
	var nodes []any
	if err := json.Unmarshal([]byte(rawBody), &nodes); err != nil {
		if len(assets) != 0 || strings.Contains(rawBody, "/"+media.StagingDirectory+"/") {
			return "", "", nil, errors.New("uploaded images require structured editor content")
		}
		return rawBody, body, old, nil
	}
	usedCanonical := make(map[string]bool)
	previousPlateURLs := plateImageURLs(oldRawBody)
	created := make([]*pb.Thumbnail, 0, len(assets))
	var visit func(any) error
	visit = func(value any) error {
		node, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		if node["type"] == "img" {
			urlValue, _ := node["url"].(string)
			originalValue, _ := node["originalUrl"].(string)
			token, _ := node["assetToken"].(string)
			if token == "" {
				if strings.Contains(urlValue, "/"+media.StagingDirectory+"/") || strings.Contains(originalValue, "/"+media.StagingDirectory+"/") {
					return errors.New("staging image is missing its asset token")
				}
				usedCanonical[urlValue] = true
				usedCanonical[originalValue] = true
			} else {
				if urlValue == "" || originalValue == "" {
					return errors.New("uploaded image is missing its staging URLs")
				}
				asset := assets[token]
				if asset == nil || asset.Kind != "image" {
					return errors.New("invalid image asset token")
				}
				var original, thumbnail stagedObject
				for _, object := range asset.Objects {
					switch object.Role {
					case "original":
						original = object
					case "thumbnail":
						thumbnail = object
					}
				}
				if original.Name == "" {
					return errors.New("image asset has no original")
				}
				if thumbnail.Name == "" {
					thumbnail = original
				}
				published, err := s.uploads.PromoteImage(&media.StagedImage{
					ThumbnailWidth: asset.Width, ThumbnailHeight: asset.Height,
					Objects: []media.StagedObject{
						{Name: original.Name, Digest: original.Digest, Extension: original.Extension, MimeType: original.MimeType, Size: original.Size, Role: "original"},
						{Name: thumbnail.Name, Digest: thumbnail.Digest, Extension: thumbnail.Extension, MimeType: thumbnail.MimeType, Size: thumbnail.Size, Role: "thumbnail"},
					},
				})
				if err != nil {
					return fmt.Errorf("promote image: %w", err)
				}
				originalURL := published.URL
				thumbURL := published.ThumbnailURL
				body = strings.ReplaceAll(body, originalValue, originalURL)
				body = strings.ReplaceAll(body, urlValue, thumbURL)
				node["url"] = thumbURL
				node["originalUrl"] = originalURL
				delete(node, "assetToken")
				created = append(created, &pb.Thumbnail{Url: thumbURL, Link: originalURL, Width: int32(asset.Width), Height: int32(asset.Height)})
			}
		}
		if children, ok := node["children"].([]any); ok {
			for _, child := range children {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, node := range nodes {
		if err := visit(node); err != nil {
			return "", "", nil, err
		}
	}
	thumbnails := make([]*pb.Thumbnail, 0, len(old)+len(created))
	for _, thumbnail := range old {
		if thumbnail == nil {
			continue
		}
		wasPlateManaged := previousPlateURLs[thumbnail.Url] || previousPlateURLs[thumbnail.Link]
		if !wasPlateManaged || usedCanonical[thumbnail.Url] || usedCanonical[thumbnail.Link] {
			thumbnails = append(thumbnails, thumbnail)
		}
	}
	thumbnails = append(thumbnails, created...)
	raw, err := json.Marshal(nodes)
	if err != nil {
		return "", "", nil, err
	}
	return string(raw), body, thumbnails, nil
}
