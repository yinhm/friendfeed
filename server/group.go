package server

import (
	"context"
	"errors"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
)

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
