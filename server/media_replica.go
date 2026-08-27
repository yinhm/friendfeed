package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
)

const mediaReplicaTaskType = "media.mirror_r2"

type mediaReplicaPayload struct {
	Key      string `json:"key"`
	MimeType string `json:"mime_type"`
}

func (s *ApiServer) registerMediaReplicaTask() error {
	return s.tasks.RegisterDefinition(mediaReplicaTaskType, taskqueue.Definition{
		ValidatePayload: func(raw []byte, version uint32) error {
			var payload mediaReplicaPayload
			if version != 1 || json.Unmarshal(raw, &payload) != nil || payload.Key == "" {
				return errors.New("invalid media replica payload")
			}
			return nil
		},
		MaxAttempts: 6, LeaseDuration: time.Minute, MaxLease: 5 * time.Minute,
		BackoffBase: time.Minute, BackoffCap: time.Hour, Handler: s.handleMediaReplicaTask,
	})
}

func (s *ApiServer) handleMediaReplicaTask(_ context.Context, task *pb.Task) error {
	if s.mediaReplica == nil {
		return nil
	}
	var payload mediaReplicaPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return err
	}
	return s.mediaReplica.PutLocal(payload.Key, payload.MimeType)
}

func collectCanonicalMediaRefs(entry *pb.Entry, mediaBaseURL string) map[string]string {
	refs := make(map[string]string)
	if entry == nil {
		return refs
	}
	for _, thumbnail := range entry.Thumbnails {
		if thumbnail == nil {
			continue
		}
		for _, raw := range []string{thumbnail.Url, thumbnail.Link} {
			if key, ok := media.CanonicalKeyFromURL(mediaBaseURL, raw); ok {
				refs[key] = "image/*"
			}
		}
	}
	for _, file := range entry.Files {
		if file != nil {
			if key, ok := media.CanonicalKeyFromURL(mediaBaseURL, file.Url); ok {
				refs[key] = file.Type
			}
		}
	}
	return refs
}

func (s *ApiServer) enqueueAddedMediaRefs(ctx context.Context, oldEntry, newEntry *pb.Entry) error {
	if s.mediaReplica == nil {
		return nil
	}
	oldRefs := collectCanonicalMediaRefs(oldEntry, s.mediaBaseURL)
	for key, mimeType := range collectCanonicalMediaRefs(newEntry, s.mediaBaseURL) {
		if _, kept := oldRefs[key]; kept {
			continue
		}
		payload, _ := json.Marshal(mediaReplicaPayload{Key: key, MimeType: mimeType})
		if _, err := s.tasks.Enqueue(ctx, taskqueue.Spec{Type: mediaReplicaTaskType, Payload: payload, PayloadVersion: 1, IdempotencyKey: "media-r2:" + key}); err != nil {
			return fmt.Errorf("enqueue media replica %s: %w", key, err)
		}
	}
	return nil
}
