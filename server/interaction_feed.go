package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const interactionScanBudget = 300

func interactionCursor(key, prefix store.Key) string {
	if !bytes.HasPrefix(key, prefix) || len(key)-len(prefix) != feedCursorPositionSize {
		return ""
	}
	return util.Base58Encode(key[len(prefix):])
}

func interactionCursorKey(raw string, prefix store.Key) (store.Key, error) {
	if raw == "" {
		return nil, nil
	}
	position, err := util.Base58Decode(raw)
	if err != nil || len(position) != feedCursorPositionSize {
		return nil, errors.New("invalid interaction cursor")
	}
	return append(append(store.Key(nil), prefix...), position...), nil
}

func (s *ApiServer) FetchInteractionFeed(ctx context.Context, req *pb.InteractionFeedRequest) (*pb.InteractionFeedResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	profileID, err := uuid.FromString(req.ProfileUuid)
	if err != nil || profileID == uuid.Nil {
		return nil, status.Error(codes.InvalidArgument, "profile_uuid is invalid")
	}
	viewerID, err := uuid.FromString(req.ViewerUuid)
	if err != nil || viewerID == uuid.Nil {
		return nil, status.Error(codes.Unauthenticated, "viewer_uuid is required")
	}
	if viewerID != profileID {
		return nil, status.Error(codes.PermissionDenied, "interaction feed is owner-only")
	}
	profile, err := model.GetProfileFromUuid(s.mdb, profileID)
	if err != nil || profile.Deleted || profile.Type != "user" {
		return nil, status.Error(codes.NotFound, "profile not found")
	}
	pageSize := int(req.PageSize)
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var prefix store.Key
	switch req.Kind {
	case pb.InteractionKind_INTERACTION_KIND_LIKE:
		prefix = model.LikeTimelinePrefix(profileID)
	case pb.InteractionKind_INTERACTION_KIND_COMMENT:
		prefix = model.CommentTimelinePrefix(profileID)
	default:
		return nil, status.Error(codes.InvalidArgument, "interaction kind is required")
	}
	cursorKey, err := interactionCursorKey(req.Cursor, prefix)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	iter, err := s.rdb.NewIterator(prefix)
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	if cursorKey != nil {
		iter.SeekGE(cursorKey)
		if iter.Valid() && bytes.Equal(iter.UnsafeRawKey(), cursorKey) {
			iter.Next()
		}
	} else {
		iter.First()
	}
	items := make([]*pb.InteractionItem, 0, pageSize+1)
	keys := make([]store.Key, 0, pageSize+1)
	resolver := newProfileResolver(s.mdb)
	scanned := 0
	var lastScanned store.Key
	for iter.Valid() && len(items) <= pageSize && scanned < interactionScanBudget {
		scanned++
		key := iter.Key()
		lastScanned = key
		_, entryID, _, parseErr := model.ParseInteractionTimelineKey(key, map[pb.InteractionKind]store.KeyPrefix{pb.InteractionKind_INTERACTION_KIND_LIKE: model.TableLikeTimeline, pb.InteractionKind_INTERACTION_KIND_COMMENT: model.TableCommentTimeline}[req.Kind])
		if parseErr != nil {
			return nil, parseErr
		}
		entry, getErr := model.GetEntry(s.rdb, entryID.String())
		if errors.Is(getErr, model.ErrNotFound) {
			_ = s.deleteInteractionOrphan(req.Kind, profileID, entryID, key)
			iter.Next()
			continue
		}
		if getErr != nil {
			return nil, getErr
		}
		if err := model.LoadEntryInteractions(s.rdb, entry); err != nil {
			return nil, err
		}
		if _, err := fmtEntryProfilesWithResolver(resolver, entry); err != nil {
			iter.Next()
			continue
		}
		item := &pb.InteractionItem{Entry: entry}
		if req.Kind == pb.InteractionKind_INTERACTION_KIND_LIKE {
			raw, err := s.rdb.Get(model.LikeKey(entryID, profileID))
			if err != nil {
				_ = s.rdb.Delete(key)
				iter.Next()
				continue
			}
			item.Like = new(pb.Like)
			if err := proto.Unmarshal(raw, item.Like); err != nil {
				return nil, err
			}
		} else {
			rawID := iter.Value()
			if len(rawID) != uuid.Size {
				return nil, fmt.Errorf("invalid CommentTimeline value length %d", len(rawID))
			}
			commentID, err := uuid.FromBytes(rawID)
			if err != nil {
				return nil, err
			}
			raw, err := s.rdb.Get(model.CommentKey(entryID, commentID))
			if err != nil {
				_ = s.deleteInteractionOrphan(req.Kind, profileID, entryID, key)
				iter.Next()
				continue
			}
			item.LatestComment = new(pb.Comment)
			if err := proto.Unmarshal(raw, item.LatestComment); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
		keys = append(keys, key)
		iter.Next()
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	response := &pb.InteractionFeedResponse{Profile: profile}
	if len(items) > pageSize {
		response.Items = items[:pageSize]
		response.NextCursor = interactionCursor(keys[pageSize-1], prefix)
	} else {
		response.Items = items
		if scanned >= interactionScanBudget && iter.Valid() {
			response.NextCursor = interactionCursor(lastScanned, prefix)
		}
	}
	return response, nil
}

func (s *ApiServer) deleteInteractionOrphan(kind pb.InteractionKind, actor, entry uuid.UUID, indexKey store.Key) error {
	return s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		if err := batch.Delete(indexKey, nil); err != nil {
			return err
		}
		if kind == pb.InteractionKind_INTERACTION_KIND_COMMENT {
			return batch.Delete(model.CommentTimelinePositionKey(actor, entry), nil)
		}
		return nil
	})
}
