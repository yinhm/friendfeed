package model

import (
	"errors"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/store"
)

// GroupAdminKey encodes the authoritative Group admin-role row: group UUID
// followed by admin user UUID. The value is always empty.
func GroupAdminKey(group, admin uuid.UUID) (store.Key, error) {
	if group == uuid.Nil || admin == uuid.Nil {
		return nil, errors.New("group UUID and admin UUID are required")
	}
	return NewKeyFrom(GroupAdmin.Prefix, group.Bytes(), admin.Bytes()), nil
}

// IsGroupAdmin reports whether user holds the admin role for group, per the
// GroupAdmin table. This is the sole authoritative source of Group admin
// role; legacy Feedinfo.Admins/Graph.admins snapshots are migration input
// only and must not be consulted once a caller has access to this resolver.
func IsGroupAdmin(db *store.Store, group, user uuid.UUID) (bool, error) {
	key, err := GroupAdminKey(group, user)
	if err != nil {
		return false, err
	}
	return db.Exists(key)
}

// ListGroupAdmins returns every admin user UUID for group, in key order.
func ListGroupAdmins(db *store.Store, group uuid.UUID) ([]uuid.UUID, error) {
	if group == uuid.Nil {
		return nil, errors.New("group UUID is required")
	}
	prefix := NewKeyFrom(GroupAdmin.Prefix, group.Bytes())
	admins := make([]uuid.UUID, 0)
	_, err := db.ForwardScan(prefix, func(_ int, key, _ []byte) error {
		suffix := key[len(prefix):]
		admin, err := uuid.FromBytes(suffix)
		if err != nil {
			return err
		}
		admins = append(admins, admin)
		return nil
	})
	return admins, err
}

// CountGroupAdmins returns the number of admin rows for group. Callers that
// must reject demoting/removing the last admin need to run this check inside
// the same atomic critical section as the mutation, not as a separate
// preceding read.
func CountGroupAdmins(db *store.Store, group uuid.UUID) (int, error) {
	admins, err := ListGroupAdmins(db, group)
	if err != nil {
		return 0, err
	}
	return len(admins), nil
}

// GroupRole describes an actor's resolved relationship to a Group, derived
// from Profile.IsSuper, Follow (membership) and GroupAdmin (admin role).
type GroupRole struct {
	IsSuper  bool
	IsMember bool
	IsAdmin  bool
}

// GroupAction is one of the operations gated by the Group permission matrix
// in docs/group.md's "可见性与投稿" section.
type GroupAction string

const (
	GroupActionView          GroupAction = "view"
	GroupActionJoin          GroupAction = "join"
	GroupActionPost          GroupAction = "post"
	GroupActionManageMembers GroupAction = "manage_members"
	GroupActionManageService GroupAction = "manage_service"
	GroupActionManageGroup   GroupAction = "manage_group"
)

// GroupActionAllowed implements the Group permission matrix: public-group
// non-member / member / admin / super, crossed with view feed, join, post,
// manage members-admin, manage FeedService, and modify-delete group. It does
// not itself enforce private-Group visibility; callers must apply the
// separate private-visibility check documented in docs/group.md before
// reaching this matrix.
func GroupActionAllowed(role GroupRole, action GroupAction) bool {
	if role.IsSuper || role.IsAdmin {
		return true
	}
	switch action {
	case GroupActionView, GroupActionJoin:
		return true
	case GroupActionPost:
		return role.IsMember
	default:
		return false
	}
}
