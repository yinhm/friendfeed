package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
		if payload.Kind == "file" {
			files = append(files, token)
		} else {
			images[token] = payload
		}
	}
	return images, files, nil
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
				originalKey, err := s.staging.Promote(original.Name, original.Digest, original.Extension, original.Size)
				if err != nil {
					return fmt.Errorf("promote image original: %w", err)
				}
				thumbKey, err := s.staging.Promote(thumbnail.Name, thumbnail.Digest, thumbnail.Extension, thumbnail.Size)
				if err != nil {
					return fmt.Errorf("promote image thumbnail: %w", err)
				}
				originalURL := strings.TrimRight(s.mediaBaseURL, "/") + "/" + originalKey
				thumbURL := strings.TrimRight(s.mediaBaseURL, "/") + "/" + thumbKey
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
