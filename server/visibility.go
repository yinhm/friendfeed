package server

import (
	"errors"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type visibilityDecision uint8

const (
	visibilityDenied visibilityDecision = iota
	visibilityAllowed
	visibilityTargetUnavailable
)

// entryVisibilityResolver is request-scoped. It resolves the viewer once and
// caches target-feed decisions, so a page containing many entries from the
// same Feed performs one Profile read and at most one Follow lookup.
type entryVisibilityResolver struct {
	server  *ApiServer
	viewer  uuid.UUID
	profile *pb.Profile
	targets map[uuid.UUID]visibilityDecision
}

func newEntryVisibilityResolver(s *ApiServer, viewerRaw string) (*entryVisibilityResolver, error) {
	r := &entryVisibilityResolver{server: s, targets: make(map[uuid.UUID]visibilityDecision)}
	if viewerRaw == "" {
		return r, nil
	}
	viewer, err := uuid.FromString(viewerRaw)
	if err != nil || viewer == uuid.Nil {
		return nil, status.Error(codes.InvalidArgument, "invalid viewer_uuid")
	}
	profile, err := model.GetProfileFromUuid(s.mdb, viewer)
	if errors.Is(err, model.ErrNotFound) || errors.Is(err, model.ErrProfileDeleted) {
		return nil, status.Error(codes.PermissionDenied, "viewer profile is unavailable")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read viewer profile: %v", err)
	}
	if profile == nil || profile.Type != "user" {
		return nil, status.Error(codes.PermissionDenied, "viewer profile is unavailable")
	}
	r.viewer = viewer
	r.profile = profile
	return r, nil
}

func (r *entryVisibilityResolver) feed(profile *pb.Profile) (visibilityDecision, error) {
	if profile == nil || profile.Deleted {
		return visibilityTargetUnavailable, nil
	}
	feed, err := uuid.FromString(profile.Uuid)
	if err != nil || feed == uuid.Nil {
		return visibilityTargetUnavailable, nil
	}
	return r.target(feed, profile)
}

func (r *entryVisibilityResolver) entry(entry *pb.Entry) (visibilityDecision, error) {
	target, ok := entryVisibilityTarget(entry)
	if !ok {
		return visibilityTargetUnavailable, nil
	}
	return r.target(target, nil)
}

func (r *entryVisibilityResolver) target(feed uuid.UUID, known *pb.Profile) (visibilityDecision, error) {
	if decision, ok := r.targets[feed]; ok {
		return decision, nil
	}
	profile := known
	if profile == nil {
		var err error
		profile, err = model.GetProfileFromUuid(r.server.mdb, feed)
		if errors.Is(err, model.ErrNotFound) || errors.Is(err, model.ErrProfileDeleted) {
			r.targets[feed] = visibilityTargetUnavailable
			return visibilityTargetUnavailable, nil
		}
		if err != nil {
			return visibilityDenied, status.Errorf(codes.Internal, "read target feed: %v", err)
		}
	}
	if profile == nil || profile.Deleted {
		r.targets[feed] = visibilityTargetUnavailable
		return visibilityTargetUnavailable, nil
	}
	if !profile.Private {
		r.targets[feed] = visibilityAllowed
		return visibilityAllowed, nil
	}
	if r.profile == nil {
		r.targets[feed] = visibilityDenied
		return visibilityDenied, nil
	}
	if r.profile.IsSuper || r.viewer == feed {
		r.targets[feed] = visibilityAllowed
		return visibilityAllowed, nil
	}
	follows, err := model.IsFollower(r.server.rdb, feed, r.viewer)
	if err != nil {
		return visibilityDenied, status.Errorf(codes.Internal, "check private feed follower: %v", err)
	}
	decision := visibilityDenied
	if follows {
		decision = visibilityAllowed
	}
	r.targets[feed] = decision
	return decision, nil
}

func visibilityReadError(decision visibilityDecision, subject string) error {
	switch decision {
	case visibilityDenied:
		return status.Errorf(codes.PermissionDenied, "access denied to %s", subject)
	case visibilityTargetUnavailable:
		return status.Errorf(codes.NotFound, "%s not found", subject)
	default:
		return nil
	}
}
