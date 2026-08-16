package server

import (
	"context"
	"errors"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
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
// target_uuid must match: joining is always self-service.
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
	if err := model.JoinGroup(s.rdb, group, target); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return &emptypb.Empty{}, nil
}

// LeaveGroup lets actor leave group on their own behalf. Rejected if actor
// currently holds the admin role; they must be demoted first.
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
	if err := model.LeaveGroup(s.rdb, group, target); err != nil {
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
// they must be demoted first.
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
	if err := model.RemoveGroupMember(s.rdb, group, target); err != nil {
		return nil, taskRPCError(errors.Join(taskqueue.ErrFailedPrecondition, err))
	}
	return &emptypb.Empty{}, nil
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
