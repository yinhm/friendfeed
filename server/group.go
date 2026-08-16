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
	taskqueue "github.com/yinhm/friendfeed/task"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// authorizeGroupManage checks that actor may manage membership/admin roles
// for group, per docs/group.md's permission matrix: only a Group admin or
// super may manage members/admins. It reads the GroupAdmin table directly,
// the sole authoritative admin-role source.
func (s *ApiServer) authorizeGroupManage(actor, group uuid.UUID) error {
	actorProfile, err := model.GetProfileFromUuid(s.rdb, actor)
	if err != nil {
		return err
	}
	if actorProfile.IsSuper {
		return nil
	}
	isAdmin, err := model.IsGroupAdmin(s.rdb, group, actor)
	if err != nil {
		return err
	}
	if !isAdmin {
		return errors.New("actor is not a Group administrator")
	}
	return nil
}

func parseGroupMembershipRequestIDs(actorRaw, groupRaw, targetRaw string) (actor, group, target uuid.UUID, err error) {
	actor, actorErr := uuid.FromString(actorRaw)
	group, groupErr := uuid.FromString(groupRaw)
	target, targetErr := uuid.FromString(targetRaw)
	if actorErr != nil || groupErr != nil || targetErr != nil || actor == uuid.Nil || group == uuid.Nil || target == uuid.Nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, errors.New("valid actor_uuid, group_uuid, and target_uuid are required")
	}
	return actor, group, target, nil
}

// JoinGroup lets actor join group on their own behalf. actor_uuid and
// target_uuid must match: joining is always self-service. The membership
// edges and the Home rebuild task commit in one Pebble batch, so a
// successful join always triggers the docs/group.md timeline rebuild.
func (s *ApiServer) JoinGroup(ctx context.Context, request *pb.GroupMembershipRequest) (*emptypb.Empty, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, group, target, err := parseGroupMembershipRequestIDs(request.ActorUuid, request.GroupUuid, request.TargetUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	if actor != target {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("JoinGroup is self-service; actor_uuid must equal target_uuid")))
	}
	spec, err := homeRebuildSpec(target)
	if err != nil {
		return nil, taskRPCError(err)
	}
	if _, err := s.tasks.EnqueueWith(ctx, []taskqueue.Spec{spec}, func(batch *pebble.Batch) error {
		return model.StageJoinGroup(s.rdb, batch, group, target)
	}); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return &emptypb.Empty{}, nil
}

// LeaveGroup lets actor leave group on their own behalf. Rejected if actor
// currently holds the admin role; they must be demoted first. The membership
// removal and the Home rebuild task commit in one Pebble batch.
func (s *ApiServer) LeaveGroup(ctx context.Context, request *pb.GroupMembershipRequest) (*emptypb.Empty, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, group, target, err := parseGroupMembershipRequestIDs(request.ActorUuid, request.GroupUuid, request.TargetUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	if actor != target {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("LeaveGroup is self-service; actor_uuid must equal target_uuid")))
	}
	spec, err := homeRebuildSpec(target)
	if err != nil {
		return nil, taskRPCError(err)
	}
	if _, err := s.tasks.EnqueueWith(ctx, []taskqueue.Spec{spec}, func(batch *pebble.Batch) error {
		return model.StageLeaveGroup(s.rdb, batch, group, target)
	}); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return &emptypb.Empty{}, nil
}

// AddGroupAdmin promotes target to admin of group. actor must already be an
// admin or super. target must already be a member.
func (s *ApiServer) AddGroupAdmin(ctx context.Context, request *pb.GroupMembershipRequest) (*emptypb.Empty, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, group, target, err := parseGroupMembershipRequestIDs(request.ActorUuid, request.GroupUuid, request.TargetUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	if err := s.authorizeGroupManage(actor, group); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	if err := model.AddGroupAdmin(s.rdb, group, target); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return &emptypb.Empty{}, nil
}

// RemoveGroupAdmin demotes target from admin of group. actor must already be
// an admin or super. Rejected if target is the Group's only admin.
func (s *ApiServer) RemoveGroupAdmin(ctx context.Context, request *pb.GroupMembershipRequest) (*emptypb.Empty, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, group, target, err := parseGroupMembershipRequestIDs(request.ActorUuid, request.GroupUuid, request.TargetUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	if err := s.authorizeGroupManage(actor, group); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	if err := model.RemoveGroupAdmin(s.rdb, group, target); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return &emptypb.Empty{}, nil
}

// RemoveGroupMember removes target's membership in group. actor must already
// be an admin or super. Rejected if target currently holds the admin role;
// they must be demoted first. The removal and the removed member's Home
// rebuild task commit in one Pebble batch.
func (s *ApiServer) RemoveGroupMember(ctx context.Context, request *pb.GroupMembershipRequest) (*emptypb.Empty, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, group, target, err := parseGroupMembershipRequestIDs(request.ActorUuid, request.GroupUuid, request.TargetUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	if err := s.authorizeGroupManage(actor, group); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	spec, err := homeRebuildSpec(target)
	if err != nil {
		return nil, taskRPCError(err)
	}
	if _, err := s.tasks.EnqueueWith(ctx, []taskqueue.Spec{spec}, func(batch *pebble.Batch) error {
		return model.StageRemoveGroupMember(s.rdb, batch, group, target)
	}); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return &emptypb.Empty{}, nil
}

// DeleteGroup soft-deletes group per docs/group.md. actor must be a Group
// admin or super; the last admin may delete the whole Group. The Profile is
// marked Deleted, which immediately blocks Join, posting and new Service
// delivery through the standard ErrProfileDeleted paths; historical content
// and relationship edges are left for bounded background cleanup.
func (s *ApiServer) DeleteGroup(ctx context.Context, request *pb.DeleteGroupRequest) (*emptypb.Empty, error) {
	if request == nil || request.GroupUuid == "" {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, err := uuid.FromString(request.ActorUuid)
	if err != nil || actor == uuid.Nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("valid actor_uuid is required")))
	}
	group, err := uuid.FromString(request.GroupUuid)
	if err != nil || group == uuid.Nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("valid group_uuid is required")))
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}
	if err := s.authorizeGroupManage(actor, group); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	if err := model.DeleteGroup(s.rdb, group); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return &emptypb.Empty{}, nil
}

// GetGroup returns one Group's metadata plus the viewer's member/admin
// status, per docs/group.md's read contract.
func (s *ApiServer) GetGroup(ctx context.Context, request *pb.GetGroupRequest) (*pb.GroupView, error) {
	if request == nil || request.GroupUuid == "" {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	group, err := uuid.FromString(request.GroupUuid)
	if err != nil || group == uuid.Nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("valid group_uuid is required")))
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}
	profile, err := model.GetProfileFromUuid(s.rdb, group)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrNotFound, err))
	}
	if profile.Type != "group" {
		return nil, taskRPCError(errors.Join(taskqueue.ErrNotFound, errors.New("profile is not a Group")))
	}
	view := &pb.GroupView{Group: profile}
	if request.ViewerUuid != "" {
		viewer, err := uuid.FromString(request.ViewerUuid)
		if err != nil {
			return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("invalid viewer_uuid")))
		}
		if view.IsMember, err = model.IsGroupMember(s.rdb, group, viewer); err != nil {
			return nil, taskRPCError(err)
		}
		if view.IsAdmin, err = model.IsGroupAdmin(s.rdb, group, viewer); err != nil {
			return nil, taskRPCError(err)
		}
	}
	return view, nil
}

// ListGroupMembers pages a Group's membership from its Follower edges,
// following the same bounded-scan cursor contract as ListUserGroups
// (docs/group_navigation.md): cursor marks the last scanned edge, a page may
// be short or empty while still carrying next_cursor.
func (s *ApiServer) ListGroupMembers(ctx context.Context, request *pb.ListGroupMembersRequest) (*pb.ListGroupMembersResponse, error) {
	if request == nil || request.GroupUuid == "" {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	group, err := uuid.FromString(request.GroupUuid)
	if err != nil || group == uuid.Nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("valid group_uuid is required")))
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}
	if _, err := model.GetProfileFromUuid(s.rdb, group); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrNotFound, err))
	}

	limit := int(request.Limit)
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}

	var cursorMember uuid.UUID
	if request.Cursor != "" {
		cursorMember, err = uuid.FromString(request.Cursor)
		if err != nil {
			return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("invalid cursor")))
		}
	}

	followerPrefix := model.NewKeyFrom(model.Follower.Prefix, group.Bytes())
	var members []*pb.GroupMember
	var lastEdge uuid.UUID
	scanned := 0

	iter, err := s.rdb.NewIterator(followerPrefix)
	if err != nil {
		return nil, taskRPCError(err)
	}
	defer iter.Close()
	// Same cursor discipline as ListUserGroups: seek to the position, step
	// past an exact match, and count only unexamined edges against the scan
	// budget.
	if cursorMember != uuid.Nil {
		cursorKey := model.NewKeyFrom(model.Follower.Prefix, group.Bytes(), cursorMember.Bytes())
		iter.SeekGE(cursorKey)
		// The cursor marks an already-examined edge; step past an exact hit.
		if iter.Valid() && bytes.Equal(iter.Key(), cursorKey) {
			iter.Next()
		}
	} else {
		iter.First()
	}
	for ; iter.Valid(); iter.Next() {
		// A full page stops before consuming this edge so the cursor always
		// marks an edge that was fully examined.
		if len(members) >= limit || scanned >= maxMembershipEdgeScan {
			break
		}
		scanned++

		memberUUID, err := uuid.FromBytes(iter.Key()[len(followerPrefix):])
		if err != nil {
			continue // Skip malformed edge
		}
		lastEdge = memberUUID

		profile, err := model.GetProfileFromUuid(s.rdb, memberUUID)
		if err != nil {
			continue // Skip missing/deleted Profile
		}
		isAdmin, err := model.IsGroupAdmin(s.rdb, group, memberUUID)
		if err != nil {
			return nil, taskRPCError(err)
		}
		members = append(members, &pb.GroupMember{Profile: profile, IsAdmin: isAdmin})
	}
	if err := iter.Error(); err != nil {
		return nil, taskRPCError(err)
	}

	response := &pb.ListGroupMembersResponse{Members: members}
	// Same contract as ListUserGroups: a cursor means the scan stopped
	// early; an exhausted scan leaves NextCursor empty.
	if iter.Valid() && lastEdge != uuid.Nil {
		response.NextCursor = lastEdge.String()
	}
	return response, nil
}

func (s *ApiServer) CreateGroup(ctx context.Context, request *pb.CreateGroupRequest) (*pb.Profile, error) {
	if request == nil || request.Id == "" || request.Name == "" {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, err := uuid.FromString(request.ActorUuid)
	if err != nil || actor == uuid.Nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("valid actor_uuid is required")))
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}
	group, err := model.CreateGroup(s.rdb, actor, request.Id, request.Name, request.Description, request.Picture, request.Private, s.rssNow().UTC())
	if err != nil {
		if errors.Is(err, model.ErrPrivateGroupUnsupported) {
			return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
		}
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return group, nil
}

func (s *ApiServer) UpdateGroup(ctx context.Context, request *pb.UpdateGroupRequest) (*pb.Profile, error) {
	if request == nil || request.GroupUuid == "" {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, err := uuid.FromString(request.ActorUuid)
	if err != nil || actor == uuid.Nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("valid actor_uuid is required")))
	}
	group, err := uuid.FromString(request.GroupUuid)
	if err != nil || group == uuid.Nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("valid group_uuid is required")))
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}
	if err := s.authorizeGroupManage(actor, group); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	updated, err := model.UpdateGroup(s.rdb, group, request.Name, request.Description, request.Picture)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return updated, nil
}

// maxMembershipEdgeScan bounds how many Follow/Follower edges one
// ListUserGroups/ListGroupMembers call may scan, per
// docs/group_navigation.md's bounded-iteration contract.
const maxMembershipEdgeScan = 1000

// ListUserGroups returns Groups the user has joined, filtered from their
// Follow edges. Iteration is streaming and bounded: limit counts returned
// Groups, at most maxMembershipEdgeScan edges are scanned per call, and the
// cursor marks the last fully examined edge (never a business ID), so a page
// may be short or empty while still carrying next_cursor.
func (s *ApiServer) ListUserGroups(ctx context.Context, request *pb.ListUserGroupsRequest) (*pb.ListUserGroupsResponse, error) {
	if request == nil || request.UserUuid == "" {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	user, err := uuid.FromString(request.UserUuid)
	if err != nil || user == uuid.Nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("valid user_uuid is required")))
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}

	limit := int(request.Limit)
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}

	var cursorFeed uuid.UUID
	if request.Cursor != "" {
		cursorFeed, err = uuid.FromString(request.Cursor)
		if err != nil {
			return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("invalid cursor")))
		}
	}

	followPrefix := model.NewKeyFrom(model.Follow.Prefix, user.Bytes())
	var groups []*pb.Profile
	var lastEdge uuid.UUID
	scanned := 0

	iter, err := s.rdb.NewIterator(followPrefix)
	if err != nil {
		return nil, taskRPCError(err)
	}
	defer iter.Close()
	// Cursor is a key position: seek straight to it and step past an exact
	// match, so the scan budget only ever counts unexamined edges and a
	// deleted cursor edge resumes at its successor.
	if cursorFeed != uuid.Nil {
		cursorKey := model.NewKeyFrom(model.Follow.Prefix, user.Bytes(), cursorFeed.Bytes())
		iter.SeekGE(cursorKey)
		// The cursor marks an already-examined edge; step past an exact hit.
		if iter.Valid() && bytes.Equal(iter.Key(), cursorKey) {
			iter.Next()
		}
	} else {
		iter.First()
	}
	for ; iter.Valid(); iter.Next() {
		// A full page stops before consuming this edge so the cursor always
		// marks an edge that was fully examined.
		if len(groups) >= limit || scanned >= maxMembershipEdgeScan {
			break
		}
		scanned++

		feedUUID, err := uuid.FromBytes(iter.Key()[len(followPrefix):])
		if err != nil {
			continue // Skip malformed edge
		}
		lastEdge = feedUUID

		profile, err := model.GetProfileFromUuid(s.rdb, feedUUID)
		if err != nil {
			continue // Skip missing/deleted Profile
		}
		if profile.Type == "group" {
			groups = append(groups, profile)
		}
	}
	if err := iter.Error(); err != nil {
		return nil, taskRPCError(err)
	}

	response := &pb.ListUserGroupsResponse{Groups: groups}
	// The scan stopped early (page full or edge budget hit), so more edges
	// may remain; the cursor lets the caller continue. An exhausted scan
	// leaves NextCursor empty even when the page is full.
	if iter.Valid() && lastEdge != uuid.Nil {
		response.NextCursor = lastEdge.String()
	}
	return response, nil
}

// canAccessPrivateGroup checks if viewer can access a private group's content.
// Returns nil if access is allowed, error otherwise.
// Access is granted to: group members, super users.
func (s *ApiServer) canAccessPrivateGroup(groupUUID, viewerUUID uuid.UUID) error {
	// Check if viewer is super
	viewer, err := model.GetProfileFromUuid(s.mdb, viewerUUID)
	if err == nil && viewer.IsSuper {
		return nil // super can access all groups
	}

	// Check if viewer is a member (Follow edge exists)
	followKey := model.NewKeyFrom(model.Follow.Prefix, viewerUUID.Bytes(), groupUUID.Bytes())
	isMember, err := s.rdb.Exists(followKey)
	if err != nil {
		return fmt.Errorf("failed to check membership: %w", err)
	}
	if !isMember {
		return fmt.Errorf("access denied: not a member of private group")
	}
	return nil
}

// enforcePrivateGroupRead applies docs/group.md's private-Group visibility to
// a feed-level read: only members and supers may read a private Group,
// regardless of how the feed was addressed.
func (s *ApiServer) enforcePrivateGroupRead(profile *pb.Profile, viewerRaw string) error {
	if profile.Type != "group" || !profile.Private {
		return nil
	}
	if viewerRaw == "" {
		return status.Errorf(codes.PermissionDenied, "authentication required for private group")
	}
	viewerUUID, err := uuid.FromString(viewerRaw)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid viewer_uuid")
	}
	groupUUID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return status.Errorf(codes.Internal, "group has invalid UUID")
	}
	if err := s.canAccessPrivateGroup(groupUUID, viewerUUID); err != nil {
		return status.Errorf(codes.PermissionDenied, "access denied to private group")
	}
	return nil
}

// privateGroupEntryVisible reports whether viewer may read content whose
// target feed is feedUUID, per docs/group.md: only a private Group restricts
// visibility, and only members and supers pass. Results are cached per
// request in cache, keyed by feed UUID. Unresolvable or deleted feeds stay
// visible here; orphan cleanup is the read path's existing lazy deletion.
func (s *ApiServer) privateGroupEntryVisible(feedUUID, viewer uuid.UUID, cache map[uuid.UUID]bool) (bool, error) {
	if visible, ok := cache[feedUUID]; ok {
		return visible, nil
	}
	visible := true
	feedProfile, err := model.GetProfileFromUuid(s.mdb, feedUUID)
	if err == nil && feedProfile.Type == "group" && feedProfile.Private {
		visible = viewer != uuid.Nil && s.canAccessPrivateGroup(feedUUID, viewer) == nil
	}
	cache[feedUUID] = visible
	return visible, nil
}

// entryVisibilityTarget extracts the entry's target feed UUID for visibility
// checks. Entries without a parseable target feed impose no restriction.
func entryVisibilityTarget(entry *pb.Entry) (uuid.UUID, bool) {
	if entry.FeedUuid == "" {
		return uuid.Nil, false
	}
	feedUUID, err := uuid.FromString(entry.FeedUuid)
	if err != nil || feedUUID == uuid.Nil {
		return uuid.Nil, false
	}
	return feedUUID, true
}
