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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	defaultFeedInspectionEntryLimit = 20
	maxFeedInspectionEntryLimit     = 100
	maxFeedStateBatch               = 100
)

func storedProfileByIdentifier(db *store.Store, identifier string) (*pb.Profile, uuid.UUID, error) {
	if identifier == "" {
		return nil, uuid.Nil, status.Error(codes.InvalidArgument, "Feed ID or UUID is required")
	}
	if profileUUID, err := uuid.FromString(identifier); err == nil && profileUUID != uuid.Nil {
		profile, getErr := model.GetStoredProfileFromUuid(db, profileUUID)
		if getErr == nil {
			return profile, profileUUID, nil
		}
		if !errors.Is(getErr, model.ErrNotFound) {
			return nil, uuid.Nil, getErr
		}
	}
	rawUUID, err := model.UserMap.GetRaw(db, []byte(identifier))
	if errors.Is(err, store.ErrNotFound) {
		return nil, uuid.Nil, status.Errorf(codes.NotFound, "Feed %q not found", identifier)
	}
	if err != nil {
		return nil, uuid.Nil, err
	}
	profileUUID, err := uuid.FromBytes(rawUUID)
	if err != nil || profileUUID == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("UserMap[%q] has invalid Profile UUID", identifier)
	}
	profile, err := model.GetStoredProfileFromUuid(db, profileUUID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, uuid.Nil, status.Errorf(codes.NotFound, "Feed %q has no Profile row", identifier)
	}
	return profile, profileUUID, err
}

func countPrefix(db *store.Store, prefix store.Key) (int64, error) {
	n, err := db.ForwardScan(prefix, func(int, []byte, []byte) error { return nil })
	return int64(n), err
}

// InspectFeed returns a bounded administrative diagnostic. It deliberately
// reads the stored Profile row directly so a soft-deleted Feed can still be
// diagnosed over the loopback-only management boundary.
func (s *ApiServer) InspectFeed(_ context.Context, req *pb.InspectFeedRequest) (*pb.InspectFeedResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	limit := int(req.EntryLimit)
	if limit == 0 {
		limit = defaultFeedInspectionEntryLimit
	}
	if limit < 0 || limit > maxFeedInspectionEntryLimit {
		return nil, status.Errorf(codes.InvalidArgument, "entry_limit must be between 0 and %d", maxFeedInspectionEntryLimit)
	}
	profile, feed, err := storedProfileByIdentifier(s.rdb, req.Feed)
	if err != nil {
		return nil, err
	}
	response := &pb.InspectFeedResponse{Profile: proto.Clone(profile).(*pb.Profile)}

	userMapValue, mapErr := model.UserMap.GetRaw(s.rdb, []byte(profile.Id))
	response.UserMapConsistent = mapErr == nil && bytes.Equal(userMapValue, feed.Bytes())
	if !response.UserMapConsistent {
		response.Warnings = append(response.Warnings, "Profile ID is not mapped back to this UUID")
	}

	counts := []struct {
		prefix store.Key
		set    func(int64)
	}{
		{model.NewKeyFrom(model.EntryIndex.Prefix, feed.Bytes()), func(n int64) { response.EntryCount = n }},
		{model.NewKeyFrom(model.Follow.Prefix, feed.Bytes()), func(n int64) { response.FollowingCount = n }},
		{model.NewKeyFrom(model.Follower.Prefix, feed.Bytes()), func(n int64) { response.FollowerCount = n }},
		{model.NewKeyFrom(model.FollowRequest.Prefix, feed.Bytes()), func(n int64) { response.PendingRequestCount = n }},
		{model.NewKeyFrom(model.FeedService.Prefix, feed.Bytes()), func(n int64) { response.ServiceCount = n }},
	}
	for _, item := range counts {
		n, countErr := countPrefix(s.rdb, item.prefix)
		if countErr != nil {
			return nil, countErr
		}
		item.set(n)
	}
	if profile.Type == "group" {
		response.GroupAdminCount, err = countPrefix(s.rdb, model.NewKeyFrom(model.GroupAdmin.Prefix, feed.Bytes()))
		if err != nil {
			return nil, err
		}
		response.GroupMemberCount = response.FollowerCount
	}

	if lastAccess, stateErr := model.TimelineLastAccess(s.rdb, feed); stateErr == nil {
		response.TimelineStateExists = true
		response.TimelineLastAccessMs = lastAccess.UnixMilli()
	} else if !errors.Is(stateErr, store.ErrNotFound) {
		response.Warnings = append(response.Warnings, fmt.Sprintf("TimelineState: %v", stateErr))
	}
	if _, archiveErr := model.GetFeedArchive(s.rdb, feed); archiveErr == nil {
		response.ArchiveExists = true
	} else if !errors.Is(archiveErr, store.ErrNotFound) {
		response.Warnings = append(response.Warnings, fmt.Sprintf("Feed archive: %v", archiveErr))
	}
	if _, dirtyErr := model.FeedArchiveDirtySince(s.rdb, feed); dirtyErr == nil {
		response.ArchiveDirty = true
	} else if !errors.Is(dirtyErr, store.ErrNotFound) {
		response.Warnings = append(response.Warnings, fmt.Sprintf("Feed archive dirty marker: %v", dirtyErr))
	}
	if record, keyErr := model.GetFeedApiKey(s.rdb, feed); keyErr == nil {
		response.FeedApiKeyExists = true
		response.FeedApiKeyActive = record.RevokedAtMs == 0 && len(record.KeyId) == 8 && len(record.SecretSha256) == 32
	} else if !errors.Is(keyErr, model.ErrNotFound) {
		response.Warnings = append(response.Warnings, fmt.Sprintf("Feed API key: %v", keyErr))
	}

	prefix := model.NewKeyFrom(model.EntryIndex.Prefix, feed.Bytes())
	_, err = s.rdb.ForwardScan(prefix, func(i int, key, _ []byte) error {
		if i >= limit {
			return &store.Error{Msg: "inspection limit reached", Code: store.StopIteration}
		}
		_, entryID, created, parseErr := model.ParseEntryIndexKey(key)
		if parseErr != nil {
			return parseErr
		}
		entry := new(pb.Entry)
		if getErr := model.Entry.Get(s.rdb, entryID.Bytes(), entry); errors.Is(getErr, model.ErrNotFound) {
			response.Warnings = append(response.Warnings, fmt.Sprintf("EntryIndex references missing Entry %s", entryID))
			return nil
		} else if getErr != nil {
			return getErr
		}
		response.Entries = append(response.Entries, &pb.FeedInspectionEntry{
			Uuid: entryID.String(), ProfileUuid: entry.ProfileUuid,
			FeedUuid: entry.FeedUuid, CreatedAtMs: created.UnixMilli(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

// UpdateFeedState applies one bounded, all-or-nothing state patch. Privacy is
// currently mutable only for live user Feeds; Group privacy remains fixed at
// creation and special/deleted profiles are rejected.
func (s *ApiServer) UpdateFeedState(_ context.Context, req *pb.UpdateFeedStateRequest) (*pb.UpdateFeedStateResponse, error) {
	if req == nil || req.Patch == nil || req.Patch.Private == nil {
		return nil, status.Error(codes.InvalidArgument, "private state patch is required")
	}
	if len(req.Feeds) == 0 || len(req.Feeds) > maxFeedStateBatch {
		return nil, status.Errorf(codes.InvalidArgument, "feeds must contain between 1 and %d identifiers", maxFeedStateBatch)
	}
	targetPrivate := req.Patch.GetPrivate()

	s.profileUpdateMu.Lock()
	defer s.profileUpdateMu.Unlock()

	buildChanges := func() ([]*pb.FeedStateChange, []*pb.Profile, error) {
		seen := make(map[uuid.UUID]struct{}, len(req.Feeds))
		changes := make([]*pb.FeedStateChange, 0, len(req.Feeds))
		profiles := make([]*pb.Profile, 0, len(req.Feeds))
		for _, identifier := range req.Feeds {
			profile, profileUUID, err := storedProfileByIdentifier(s.rdb, identifier)
			if err != nil {
				return nil, nil, err
			}
			if _, exists := seen[profileUUID]; exists {
				continue
			}
			seen[profileUUID] = struct{}{}
			if profile.Deleted {
				return nil, nil, status.Errorf(codes.FailedPrecondition, "Feed %q is deleted", profile.Id)
			}
			if profile.Type != "user" {
				return nil, nil, status.Errorf(codes.FailedPrecondition, "Feed %q has immutable privacy because its type is %q", profile.Id, profile.Type)
			}
			changes = append(changes, &pb.FeedStateChange{
				Uuid: profile.Uuid, Id: profile.Id, Type: profile.Type,
				Before:  &pb.FeedState{Private: profile.Private, Deleted: profile.Deleted},
				After:   &pb.FeedState{Private: targetPrivate, Deleted: profile.Deleted},
				Changed: profile.Private != targetPrivate,
			})
			profiles = append(profiles, profile)
		}
		return changes, profiles, nil
	}

	changes, profiles, err := buildChanges()
	if err != nil {
		return nil, err
	}
	if req.DryRun {
		return &pb.UpdateFeedStateResponse{Changes: changes}, nil
	}
	err = s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		// Re-resolve inside ApplyBatch's serialization boundary. The coarse
		// profile lock excludes all normal server mutations; this second read
		// also keeps the staged values tied to the current persisted rows.
		var refreshErr error
		changes, profiles, refreshErr = buildChanges()
		if refreshErr != nil {
			return refreshErr
		}
		for _, profile := range profiles {
			if profile.Private == targetPrivate {
				continue
			}
			if _, stageErr := model.StageSetUserFeedPrivacy(s.rdb, batch, profile, targetPrivate); stageErr != nil {
				return stageErr
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &pb.UpdateFeedStateResponse{Changes: changes}, nil
}
