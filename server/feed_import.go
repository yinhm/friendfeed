package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/internal/feedprincipal"
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	maxImportTitleBytes = 512
	maxImportBodyBytes  = 256 << 10
)

func decimalID(raw string) bool {
	if raw == "" || len(raw) > 64 {
		return false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func twitterItemIDFromURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "twitter.com", "www.twitter.com", "mobile.twitter.com", "x.com", "www.x.com":
	default:
		return "", false
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) < 3 {
		return "", false
	}
	segment := strings.ToLower(parts[len(parts)-2])
	if segment != "status" && segment != "statuses" {
		return "", false
	}
	id, err := url.PathUnescape(parts[len(parts)-1])
	if err != nil || !decimalID(id) {
		return "", false
	}
	return id, true
}

func entryHasTwitterItem(entry *pb.Entry, itemID string) bool {
	if entry == nil {
		return false
	}
	values := []string{entry.Url, entry.RawLink}
	if entry.Via != nil {
		values = append(values, entry.Via.Url)
	}
	for _, raw := range values {
		if got, ok := twitterItemIDFromURL(raw); ok && got == itemID {
			return true
		}
	}
	return false
}

func canonicalEntryTarget(entry *pb.Entry) string {
	if entry != nil && entry.FeedUuid != "" {
		return entry.FeedUuid
	}
	if entry != nil {
		return entry.ProfileUuid
	}
	return ""
}

func (s *ApiServer) validateImportMedia(request *pb.ImportFeedEntryRequest) error {
	if len(request.Thumbnails)+len(request.Files) > media.MaxEntryAttachments {
		return status.Error(codes.InvalidArgument, "too many media attachments")
	}
	for _, thumbnail := range request.Thumbnails {
		if thumbnail == nil {
			return status.Error(codes.InvalidArgument, "invalid media reference")
		}
		for _, raw := range []string{thumbnail.Url, thumbnail.Link} {
			if _, ok := media.CanonicalKeyFromURL(s.mediaBaseURL, raw); !ok {
				return status.Error(codes.InvalidArgument, "invalid media reference")
			}
		}
	}
	for _, file := range request.Files {
		if file == nil {
			return status.Error(codes.InvalidArgument, "invalid media reference")
		}
		if _, ok := media.CanonicalKeyFromURL(s.mediaBaseURL, file.Url); !ok {
			return status.Error(codes.InvalidArgument, "invalid media reference")
		}
	}
	return nil
}

func (s *ApiServer) importReplay(entry *pb.Entry, target uuid.UUID, itemID string) error {
	if canonicalEntryTarget(entry) != target.String() || !entryHasTwitterItem(entry, itemID) {
		return status.Error(codes.AlreadyExists, "source identity conflict")
	}
	return nil
}

// ImportFeedEntry creates or replays one historical external item for the
// authenticated Feed capability. It intentionally does not accept a target
// Feed in the request and does not move realtime activity timelines.
func (s *ApiServer) ImportFeedEntry(ctx context.Context, request *pb.ImportFeedEntryRequest) (*pb.ImportFeedEntryResponse, error) {
	principal, ok := feedprincipal.FromIncoming(ctx)
	if !ok {
		return nil, status.Error(codes.PermissionDenied, "Feed capability is required")
	}
	if request == nil || request.SourceKind != "twitter" || !decimalID(request.SourceAccountId) || !decimalID(request.SourceItemId) {
		return nil, status.Error(codes.InvalidArgument, "invalid source identity")
	}
	if !utf8.ValidString(request.Title) || !utf8.ValidString(request.BodyHtml) || len(request.Title) > maxImportTitleBytes || len(request.BodyHtml) > maxImportBodyBytes {
		return nil, status.Error(codes.InvalidArgument, "invalid import content")
	}
	if got, ok := twitterItemIDFromURL(request.SourceUrl); !ok || got != request.SourceItemId {
		return nil, status.Error(codes.InvalidArgument, "invalid source URL")
	}
	published, err := time.Parse(time.RFC3339Nano, request.PublishedAt)
	if err != nil || published.After(time.Now().UTC().Add(5*time.Minute)) {
		return nil, status.Error(codes.InvalidArgument, "invalid published time")
	}
	if strings.TrimSpace(request.Title) == "" && strings.TrimSpace(request.BodyHtml) == "" && len(request.Thumbnails) == 0 && len(request.Files) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty import")
	}
	if err := s.validateImportMedia(request); err != nil {
		return nil, err
	}

	s.entryLifecycleMu.Lock()
	defer s.entryLifecycleMu.Unlock()
	profile, err := model.GetProfileFromUuid(s.mdb, principal.FeedUUID)
	if errors.Is(err, model.ErrNotFound) || errors.Is(err, model.ErrProfileDeleted) {
		return nil, status.Error(codes.NotFound, "Feed not found")
	}
	if err != nil {
		return nil, err
	}
	newID := model.UniqueKeyFrom("external-entry", principal.FeedUUID.String(), request.SourceKind, request.SourceItemId)
	if existing, getErr := model.GetEntry(s.rdb, newID.String()); getErr == nil {
		if err := s.importReplay(existing, principal.FeedUUID, request.SourceItemId); err != nil {
			return nil, err
		}
		return &pb.ImportFeedEntryResponse{Entry: existing}, nil
	} else if !errors.Is(getErr, model.ErrNotFound) {
		return nil, getErr
	}
	legacyID := model.UniqueKeyFrom("twitter", request.SourceItemId)
	if existing, getErr := model.GetEntry(s.rdb, legacyID.String()); getErr == nil {
		if canonicalEntryTarget(existing) == principal.FeedUUID.String() {
			if err := s.importReplay(existing, principal.FeedUUID, request.SourceItemId); err != nil {
				return nil, err
			}
			return &pb.ImportFeedEntryResponse{Entry: existing, LegacyReplay: true}, nil
		}
	} else if !errors.Is(getErr, model.ErrNotFound) {
		return nil, getErr
	}

	entry := &pb.Entry{
		Id: newID.String(), Url: request.SourceUrl, RawLink: request.SourceUrl,
		Date: published.UTC().Format(time.RFC3339Nano), Title: request.Title, Body: request.BodyHtml,
		ProfileUuid: principal.FeedUUID.String(), FeedUuid: principal.FeedUUID.String(),
		From:       &pb.Feed{Uuid: profile.Uuid, Id: profile.Id, Name: profile.Name, Type: profile.Type, Picture: profile.Picture},
		Via:        &pb.Via{Name: "Twitter", Url: request.SourceUrl},
		Thumbnails: proto.Clone(&pb.Entry{Thumbnails: request.Thumbnails}).(*pb.Entry).Thumbnails,
		Files:      proto.Clone(&pb.Entry{Files: request.Files}).(*pb.Entry).Files,
	}
	if _, err := model.PutArchiveEntry(s.rdb, entry); err != nil {
		return nil, fmt.Errorf("store imported Entry: %w", err)
	}
	if err := s.enqueueAddedMediaRefs(ctx, nil, entry); err != nil {
		slog.Error("import_media_mirror_enqueue_failed", "entry_uuid", entry.Id, "error", err)
	}
	return &pb.ImportFeedEntryResponse{Entry: entry, Created: true}, nil
}
