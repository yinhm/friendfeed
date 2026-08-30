package server

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	taskqueue "github.com/yinhm/friendfeed/task"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// authorizeGroupManage checks that actor may manage membership/admin roles
// for group, per docs/group.md's permission matrix: only a Group admin or
// super may manage members/admins. It reads the GroupAdmin table directly,
// the sole authoritative admin-role source.
func (s *ApiServer) authorizeGroupManage(actor, group uuid.UUID) error {
	groupProfile, err := model.GetProfileFromUuid(s.rdb, group)
	if err != nil {
		return err
	}
	if groupProfile.Type != "group" {
		return errors.New("target profile is not a Group")
	}
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
// edges and the single-Feed Home add task commit in one Pebble batch.
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
	spec, err := newHomeFeedTask(target, group, homeFeedActionAdd)
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
// removal and the single-Feed Home remove task commit in one Pebble batch.
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
	spec, err := newHomeFeedTask(target, group, homeFeedActionRemove)
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
// admin or super. target must already be a member. Only the real non-admin ->
// admin transition emits GROUP_ADMIN_ADDED, atomically with GroupAdmin.
func (s *ApiServer) AddGroupAdmin(ctx context.Context, request *pb.GroupMembershipRequest) (*emptypb.Empty, error) {
	s.profileUpdateMu.Lock()
	defer s.profileUpdateMu.Unlock()
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, group, target, err := parseGroupMembershipRequestIDs(request.ActorUuid, request.GroupUuid, request.TargetUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	activity := time.Now().UTC()
	var notification notificationStageResult
	if err := s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		if err := s.authorizeGroupManage(actor, group); err != nil {
			return err
		}
		wasAdmin, err := model.IsGroupAdmin(s.rdb, group, target)
		if err != nil {
			return err
		}
		if err := model.StageAddGroupAdmin(s.rdb, batch, group, target); err != nil {
			return err
		}
		if wasAdmin {
			return nil
		}
		var stageErr error
		notification, stageErr = s.stageGroupTransitionNotification(batch, model.NotificationGroupAdminAdded, actor, group, target, activity)
		return stageErr
	}); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	s.finishNotificationStage(notification)
	return &emptypb.Empty{}, nil
}

// RemoveGroupAdmin demotes target from admin of group. actor must already be
// an admin or super. Rejected if target is the Group's only admin. Only a
// real admin -> member transition emits GROUP_ADMIN_REMOVED.
func (s *ApiServer) RemoveGroupAdmin(ctx context.Context, request *pb.GroupMembershipRequest) (*emptypb.Empty, error) {
	s.profileUpdateMu.Lock()
	defer s.profileUpdateMu.Unlock()
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, group, target, err := parseGroupMembershipRequestIDs(request.ActorUuid, request.GroupUuid, request.TargetUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	activity := time.Now().UTC()
	var notification notificationStageResult
	if err := s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		if err := s.authorizeGroupManage(actor, group); err != nil {
			return err
		}
		wasAdmin, err := model.IsGroupAdmin(s.rdb, group, target)
		if err != nil {
			return err
		}
		if err := model.StageRemoveGroupAdmin(s.rdb, batch, group, target); err != nil {
			return err
		}
		if !wasAdmin {
			return nil
		}
		var stageErr error
		notification, stageErr = s.stageGroupTransitionNotification(batch, model.NotificationGroupAdminRemoved, actor, group, target, activity)
		return stageErr
	}); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	s.finishNotificationStage(notification)
	return &emptypb.Empty{}, nil
}

// RemoveGroupMember removes target's membership in group. actor must already
// be an admin or super. Rejected if target currently holds the admin role;
// they must be demoted first. The removal, removed member's Home remove task,
// and GROUP_MEMBER_REMOVED notification commit in one Pebble batch, but only
// when the target was actually a member.
func (s *ApiServer) RemoveGroupMember(ctx context.Context, request *pb.GroupMembershipRequest) (*emptypb.Empty, error) {
	if request == nil {
		return nil, taskRPCError(taskqueue.ErrInvalidArgument)
	}
	actor, group, target, err := parseGroupMembershipRequestIDs(request.ActorUuid, request.GroupUuid, request.TargetUuid)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, err))
	}
	spec, err := newHomeFeedTask(target, group, homeFeedActionRemove)
	if err != nil {
		return nil, taskRPCError(err)
	}
	activity := time.Now().UTC()
	var notification notificationStageResult
	if _, err := s.tasks.EnqueueWith(ctx, []taskqueue.Spec{spec}, func(batch *pebble.Batch) error {
		if err := s.authorizeGroupManage(actor, group); err != nil {
			return err
		}
		wasMember, err := model.IsGroupMember(s.rdb, group, target)
		if err != nil {
			return err
		}
		if err := model.StageRemoveGroupMember(s.rdb, batch, group, target); err != nil {
			return err
		}
		if !wasMember {
			return nil
		}
		var stageErr error
		notification, stageErr = s.stageGroupTransitionNotification(batch, model.NotificationGroupMemberRemoved, actor, group, target, activity)
		return stageErr
	}); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	s.finishNotificationStage(notification)
	return &emptypb.Empty{}, nil
}

// DeleteGroup soft-deletes group per docs/group.md. actor must be a Group
// admin or super; the last admin may delete the whole Group. The Profile is
// marked Deleted, which immediately blocks Join, posting and new Service
// delivery through the standard ErrProfileDeleted paths; historical content
// and relationship edges are left for bounded background cleanup.
func (s *ApiServer) DeleteGroup(ctx context.Context, request *pb.DeleteGroupRequest) (*emptypb.Empty, error) {
	s.profileUpdateMu.Lock()
	defer s.profileUpdateMu.Unlock()
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
	if err := s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		if err := s.authorizeGroupManage(actor, group); err != nil {
			return err
		}
		return model.StageDeleteGroup(s.rdb, batch, group)
	}); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return &emptypb.Empty{}, nil
}

// GetGroup returns one Group's metadata plus the viewer's member/admin
// status, per docs/group.md's read contract. Metadata (name, picture,
// description) is deliberately returned to anyone, including anonymous
// callers: it carries no content, and the follow-request flow needs it as
// its entry point (the SSR private-feed page shows the same metadata to
// anonymous visitors). Content reads stay closed via enforcePrivateFeedRead.
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
		if err != nil || viewer == uuid.Nil {
			return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("invalid viewer_uuid")))
		}
		if view.IsMember, err = model.IsGroupMember(s.rdb, group, viewer); err != nil {
			return nil, taskRPCError(err)
		}
		if view.IsAdmin, err = model.IsGroupAdmin(s.rdb, group, viewer); err != nil {
			return nil, taskRPCError(err)
		}
		if view.HasPendingRequest, err = model.IsFollowRequestPending(s.rdb, group, viewer); err != nil {
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
	profile, err := model.GetProfileFromUuid(s.rdb, group)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrNotFound, err))
	}
	if profile.Type != "group" {
		return nil, taskRPCError(errors.Join(taskqueue.ErrNotFound, errors.New("profile is not a Group")))
	}
	if err := s.enforcePrivateFeedRead(profile, request.ViewerUuid); err != nil {
		return nil, err
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
	var updated *pb.Profile
	err = s.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		if err := s.authorizeGroupManage(actor, group); err != nil {
			return err
		}
		var stageErr error
		updated, stageErr = model.StageUpdateGroup(s.rdb, batch, group, request.Name, request.Description, request.Picture)
		return stageErr
	})
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return updated, nil
}

// maxMembershipEdgeScan bounds how many Follow/Follower edges one
// ListUserGroups/ListGroupMembers call may scan, per
// docs/group_navigation.md's bounded-iteration contract.
const maxMembershipEdgeScan = 1000

const maxGroupIndexScan = 300

// ListGroups returns public Group metadata in GroupIndex activity order. The
// index is the only scan source; Profile point reads validate each derived row
// without calculating viewer-specific relationship state.
func (s *ApiServer) ListGroups(ctx context.Context, request *pb.ListGroupsRequest) (*pb.ListGroupsResponse, error) {
	if request == nil {
		request = &pb.ListGroupsRequest{}
	}
	if err := ctx.Err(); err != nil {
		return nil, taskRPCError(err)
	}
	limit := int(request.Limit)
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	scanLimit := max(limit*3, 100)
	if scanLimit > maxGroupIndexScan {
		scanLimit = maxGroupIndexScan
	}

	var cursorKey store.Key
	if request.Cursor != "" {
		position, err := util.Base58Decode(request.Cursor)
		if err != nil || len(position) != 8+uuid.Size {
			return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("invalid cursor")))
		}
		cursorKey = model.NewKeyFrom(model.GroupIndex.Prefix, position)
		if _, _, err := model.ParseGroupIndexKey(cursorKey); err != nil {
			return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("invalid cursor")))
		}
	}

	iter, err := s.rdb.NewIterator(model.GroupIndex.Prefix)
	if err != nil {
		return nil, taskRPCError(err)
	}
	defer iter.Close()
	if cursorKey == nil {
		iter.First()
	} else {
		iter.SeekGE(cursorKey)
		if iter.Valid() && bytes.Equal(iter.UnsafeKey(), cursorKey) {
			iter.Next()
		}
	}

	groups := make([]*pb.Profile, 0, limit)
	var lastPosition []byte
	scanned := 0
	for iter.Valid() && scanned < scanLimit && len(groups) < limit {
		if err := ctx.Err(); err != nil {
			return nil, taskRPCError(err)
		}
		key := iter.UnsafeKey()
		group, _, err := model.ParseGroupIndexKey(key)
		if err != nil {
			return nil, taskRPCError(err)
		}
		lastPosition = append(lastPosition[:0], key[len(model.GroupIndex.Prefix):]...)
		scanned++
		profile, err := model.GetProfileFromUuid(s.rdb, group)
		if err == nil && profile.Type == "group" && !profile.Deleted {
			groups = append(groups, profile)
		} else if err != nil && !errors.Is(err, model.ErrNotFound) && !errors.Is(err, model.ErrProfileDeleted) {
			return nil, taskRPCError(err)
		}
		iter.Next()
	}
	if err := iter.Error(); err != nil {
		return nil, taskRPCError(err)
	}
	response := &pb.ListGroupsResponse{Groups: groups}
	if iter.Valid() && len(lastPosition) > 0 {
		response.NextCursor = util.Base58Encode(lastPosition)
	}
	return response, nil
}

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
	userProfile, err := model.GetProfileFromUuid(s.rdb, user)
	if err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrNotFound, err))
	}
	if userProfile.Type != "user" {
		return nil, taskRPCError(errors.Join(taskqueue.ErrInvalidArgument, errors.New("profile is not a user")))
	}
	if request.OrderByActivity {
		rows, activityErr := model.GetGroupActivity(s.rdb, user)
		if activityErr == nil {
			limit := int(request.Limit)
			if limit <= 0 {
				limit = 10
			}
			if limit > 200 {
				limit = 200
			}
			groups := make([]*pb.Profile, 0, min(limit, len(rows)))
			for _, row := range rows {
				if len(groups) >= limit {
					break
				}
				group, parseErr := uuid.FromString(row.GroupUUID)
				if parseErr != nil || group == uuid.Nil {
					continue
				}
				member, memberErr := model.IsGroupMember(s.rdb, group, user)
				if memberErr != nil {
					return nil, taskRPCError(memberErr)
				}
				if !member {
					continue
				}
				profile, profileErr := model.GetProfileFromUuid(s.rdb, group)
				if profileErr == nil && profile.Type == "group" {
					groups = append(groups, profile)
				}
			}
			return &pb.ListUserGroupsResponse{Groups: groups}, nil
		}
		if !errors.Is(activityErr, store.ErrNotFound) {
			return nil, taskRPCError(activityErr)
		}
		// Pre-migration fallback: retain the ordinary membership listing until
		// rebuild_group_activity creates the materialized ranking.
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

// enforcePrivateFeedRead applies the private-feed visibility rule to a
// feed-level read through the same request-scoped identity semantics used by
// Entry reads. It remains as the narrow adapter used by Group member APIs.
func (s *ApiServer) enforcePrivateFeedRead(profile *pb.Profile, viewerRaw string) error {
	resolver, err := newEntryVisibilityResolver(s, viewerRaw)
	if err != nil {
		return err
	}
	decision, err := resolver.feed(profile)
	if err != nil {
		return err
	}
	if decision == visibilityDenied && viewerRaw == "" && profile != nil && profile.Private {
		return status.Error(codes.PermissionDenied, "authentication required for private feed")
	}
	return visibilityReadError(decision, "private feed")
}

// entryVisibilityTarget extracts the entry's target feed UUID for visibility
// checks: FeedUuid when set (Group or cross-posted entries), otherwise the
// author's own feed. Entries without a parseable target impose no restriction.
func entryVisibilityTarget(entry *pb.Entry) (uuid.UUID, bool) {
	targetRaw := entry.FeedUuid
	if targetRaw == "" {
		targetRaw = entry.ProfileUuid
	}
	if targetRaw == "" {
		return uuid.Nil, false
	}
	feedUUID, err := uuid.FromString(targetRaw)
	if err != nil || feedUUID == uuid.Nil {
		return uuid.Nil, false
	}
	return feedUUID, true
}
