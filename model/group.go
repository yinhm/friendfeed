package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
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

// ErrPrivateGroupUnsupported is returned when a caller attempts to create a
// private Group before the approval/invite flow exists. docs/group.md
// requires rejecting private=true outright rather than creating a Group
// members can't yet join or read through a closed loop.
var ErrPrivateGroupUnsupported = errors.New("private Group creation is not yet supported")

// StageCreateGroup stages the atomic multi-write CreateGroup mutation
// documented in docs/group.md: Profile(Type=group), Group ID UserMap,
// creator->Group Follow, Group->creator Follower, and the creator's
// GroupAdmin row, all in the caller-supplied batch. The creator must be an
// existing, non-deleted user Profile. Group IDs share the user ID
// namespace/validation/UserRenameMap reservation rules via
// NormalizeProfileId/FindProfileRenameByOldId.
func StageCreateGroup(db *store.Store, batch *pebble.Batch, actor uuid.UUID, id, name, description, picture string, private bool, now time.Time) (*pb.Profile, error) {
	if batch == nil || actor == uuid.Nil {
		return nil, errors.New("batch and actor UUID are required")
	}
	if private {
		return nil, ErrPrivateGroupUnsupported
	}
	if now.IsZero() {
		return nil, errors.New("group creation time is invalid")
	}

	actorProfile, err := GetProfileFromUuid(db, actor)
	if err != nil {
		return nil, fmt.Errorf("actor profile not found: %w", err)
	}
	if actorProfile.Type != "user" {
		return nil, errors.New("Group creator must be an existing user Profile")
	}

	groupID, err := NormalizeProfileId(id)
	if err != nil {
		return nil, fmt.Errorf("invalid Group ID: %w", err)
	}
	if reservedBy, err := FindProfileRenameByOldId(db, groupID); err == nil {
		return nil, fmt.Errorf("Group ID %q is reserved by a previous rename of profile %s", groupID, reservedBy)
	} else if !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("check Group ID %q against UserRenameMap: %w", groupID, err)
	}
	mapKey := NewKeyFrom(TableUserMap.Bytes(), []byte(groupID))
	if existing, err := db.Get(mapKey); err == nil && len(existing) > 0 {
		return nil, fmt.Errorf("Group ID %q is already taken", groupID)
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("read UserMap[%s]: %w", groupID, err)
	}

	groupUUID, err := uuid.NewV4()
	if err != nil {
		return nil, fmt.Errorf("generate Group UUID: %w", err)
	}

	group := &pb.Profile{
		Uuid:        groupUUID.String(),
		Id:          groupID,
		Name:        name,
		Type:        "group",
		Private:     false,
		Picture:     picture,
		Description: description,
	}

	if err := setProto(batch, Profile.PrefixAppend(groupUUID.Bytes()), group); err != nil {
		return nil, fmt.Errorf("stage Profile: %w", err)
	}
	if err := batch.Set(mapKey, groupUUID.Bytes(), nil); err != nil {
		return nil, fmt.Errorf("stage UserMap[%s]: %w", groupID, err)
	}
	followKey := NewKeyFrom(Follow.Prefix, actor.Bytes(), groupUUID.Bytes())
	if err := batch.Set(followKey, []byte("1"), nil); err != nil {
		return nil, fmt.Errorf("stage Follow: %w", err)
	}
	followerKey := NewKeyFrom(Follower.Prefix, groupUUID.Bytes(), actor.Bytes())
	if err := batch.Set(followerKey, []byte("1"), nil); err != nil {
		return nil, fmt.Errorf("stage Follower: %w", err)
	}
	adminKey, err := GroupAdminKey(groupUUID, actor)
	if err != nil {
		return nil, err
	}
	if err := batch.Set(adminKey, nil, nil); err != nil {
		return nil, fmt.Errorf("stage GroupAdmin: %w", err)
	}

	return group, nil
}

// CreateGroup opens an atomic batch and applies StageCreateGroup. Any failure
// leaves no partial Group residue.
func CreateGroup(db *store.Store, actor uuid.UUID, id, name, description, picture string, private bool, now time.Time) (*pb.Profile, error) {
	var group *pb.Profile
	err := db.ApplyBatch(func(batch *pebble.Batch) error {
		created, err := StageCreateGroup(db, batch, actor, id, name, description, picture, private, now)
		group = created
		return err
	})
	if err != nil {
		return nil, err
	}
	return group, nil
}
