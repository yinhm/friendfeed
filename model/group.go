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

// ErrLastGroupAdmin is returned when a mutation would leave a live Group
// with no admin at all.
var ErrLastGroupAdmin = errors.New("cannot remove the last Group admin")

// ErrGroupAdminMustBeDemotedFirst is returned when a Leave/RemoveMember
// mutation targets a user who currently holds the admin role: docs/group.md
// requires an explicit demotion before that user's membership can be
// removed, whether the removal is self-service (Leave) or admin-initiated
// (RemoveMember).
var ErrGroupAdminMustBeDemotedFirst = errors.New("Group admin must be demoted before membership can be removed")

// ErrGroupNotFound wraps a lookup failure for a Profile expected to be an
// existing Group.
var ErrGroupNotFound = errors.New("Group not found")

func getGroupProfile(db *store.Store, group uuid.UUID) (*pb.Profile, error) {
	profile, err := GetProfileFromUuid(db, group)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGroupNotFound, err)
	}
	if profile.Type != "group" {
		return nil, fmt.Errorf("%w: Profile %s is not a Group", ErrGroupNotFound, group)
	}
	return profile, nil
}

func getGroupMemberProfile(db *store.Store, user uuid.UUID) (*pb.Profile, error) {
	profile, err := GetProfileFromUuid(db, user)
	if err != nil {
		return nil, fmt.Errorf("member profile not found: %w", err)
	}
	if profile.Type != "user" {
		return nil, errors.New("Group members must be real user Profiles")
	}
	return profile, nil
}

// IsGroupMember reports whether user currently follows group, i.e. has
// joined it. Follow/Follower is the sole membership source; there is no
// separate Member table.
func IsGroupMember(db *store.Store, group, user uuid.UUID) (bool, error) {
	if group == uuid.Nil || user == uuid.Nil {
		return false, errors.New("group UUID and user UUID are required")
	}
	return db.Exists(NewKeyFrom(Follow.Prefix, user.Bytes(), group.Bytes()))
}

// StageJoinGroup stages the Follow/Follower edges that make user a member of
// group. It is idempotent: joining an already-joined Group succeeds without
// rewriting the edges. Private Groups are rejected outright until the
// approval/invite flow exists, matching CreateGroup's private=true
// rejection.
func StageJoinGroup(db *store.Store, batch *pebble.Batch, group, user uuid.UUID) error {
	if batch == nil || group == uuid.Nil || user == uuid.Nil {
		return errors.New("batch, group UUID, and user UUID are required")
	}
	groupProfile, err := getGroupProfile(db, group)
	if err != nil {
		return err
	}
	if groupProfile.Private {
		return ErrPrivateGroupUnsupported
	}
	if _, err := getGroupMemberProfile(db, user); err != nil {
		return err
	}
	alreadyMember, err := IsGroupMember(db, group, user)
	if err != nil {
		return err
	}
	if alreadyMember {
		return nil
	}
	followKey := NewKeyFrom(Follow.Prefix, user.Bytes(), group.Bytes())
	if err := batch.Set(followKey, []byte("1"), nil); err != nil {
		return fmt.Errorf("stage Follow: %w", err)
	}
	followerKey := NewKeyFrom(Follower.Prefix, group.Bytes(), user.Bytes())
	if err := batch.Set(followerKey, []byte("1"), nil); err != nil {
		return fmt.Errorf("stage Follower: %w", err)
	}
	return nil
}

// JoinGroup opens an atomic batch and applies StageJoinGroup.
func JoinGroup(db *store.Store, group, user uuid.UUID) error {
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageJoinGroup(db, batch, group, user)
	})
}

// stageRemoveGroupMembership removes user's Follow/Follower membership edges
// for group. It rejects removing a current admin's membership outright: both
// self-service Leave and admin-initiated RemoveMember must demote first,
// per docs/group.md.
func stageRemoveGroupMembership(db *store.Store, batch *pebble.Batch, group, user uuid.UUID) error {
	if batch == nil || group == uuid.Nil || user == uuid.Nil {
		return errors.New("batch, group UUID, and user UUID are required")
	}
	isAdmin, err := IsGroupAdmin(db, group, user)
	if err != nil {
		return err
	}
	if isAdmin {
		return ErrGroupAdminMustBeDemotedFirst
	}
	isMember, err := IsGroupMember(db, group, user)
	if err != nil {
		return err
	}
	if !isMember {
		return nil
	}
	followKey := NewKeyFrom(Follow.Prefix, user.Bytes(), group.Bytes())
	if err := batch.Delete(followKey, nil); err != nil {
		return fmt.Errorf("stage Follow removal: %w", err)
	}
	followerKey := NewKeyFrom(Follower.Prefix, group.Bytes(), user.Bytes())
	if err := batch.Delete(followerKey, nil); err != nil {
		return fmt.Errorf("stage Follower removal: %w", err)
	}
	return nil
}

// StageLeaveGroup stages user voluntarily leaving group. Idempotent if the
// user is not currently a member. Rejects with ErrGroupAdminMustBeDemotedFirst
// if user currently holds the admin role.
func StageLeaveGroup(db *store.Store, batch *pebble.Batch, group, user uuid.UUID) error {
	return stageRemoveGroupMembership(db, batch, group, user)
}

// LeaveGroup opens an atomic batch and applies StageLeaveGroup.
func LeaveGroup(db *store.Store, group, user uuid.UUID) error {
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageLeaveGroup(db, batch, group, user)
	})
}

// StageRemoveGroupMember stages an admin-initiated removal of target's
// membership in group. Rejects with ErrGroupAdminMustBeDemotedFirst if
// target currently holds the admin role; the caller must demote first.
// Caller-side authorization (that actor is an admin/super) is not this
// function's concern.
func StageRemoveGroupMember(db *store.Store, batch *pebble.Batch, group, target uuid.UUID) error {
	return stageRemoveGroupMembership(db, batch, group, target)
}

// RemoveGroupMember opens an atomic batch and applies StageRemoveGroupMember.
func RemoveGroupMember(db *store.Store, group, target uuid.UUID) error {
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRemoveGroupMember(db, batch, group, target)
	})
}

// StageAddGroupAdmin stages promoting target to admin of group. Promotion
// requires target to already be a member (Follow must exist first); it is
// idempotent if target is already an admin.
func StageAddGroupAdmin(db *store.Store, batch *pebble.Batch, group, target uuid.UUID) error {
	if batch == nil || group == uuid.Nil || target == uuid.Nil {
		return errors.New("batch, group UUID, and target UUID are required")
	}
	isMember, err := IsGroupMember(db, group, target)
	if err != nil {
		return err
	}
	if !isMember {
		return errors.New("target must already be a Group member before promotion to admin")
	}
	adminKey, err := GroupAdminKey(group, target)
	if err != nil {
		return err
	}
	if err := batch.Set(adminKey, nil, nil); err != nil {
		return fmt.Errorf("stage GroupAdmin: %w", err)
	}
	return nil
}

// AddGroupAdmin opens an atomic batch and applies StageAddGroupAdmin.
func AddGroupAdmin(db *store.Store, group, target uuid.UUID) error {
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageAddGroupAdmin(db, batch, group, target)
	})
}

// StageRemoveGroupAdmin stages demoting target from admin of group. Rejects
// with ErrLastGroupAdmin if target is the Group's only admin: an undeleted
// Group must always retain at least one admin. Idempotent if target is not
// currently an admin.
func StageRemoveGroupAdmin(db *store.Store, batch *pebble.Batch, group, target uuid.UUID) error {
	if batch == nil || group == uuid.Nil || target == uuid.Nil {
		return errors.New("batch, group UUID, and target UUID are required")
	}
	isAdmin, err := IsGroupAdmin(db, group, target)
	if err != nil {
		return err
	}
	if !isAdmin {
		return nil
	}
	count, err := CountGroupAdmins(db, group)
	if err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastGroupAdmin
	}
	adminKey, err := GroupAdminKey(group, target)
	if err != nil {
		return err
	}
	if err := batch.Delete(adminKey, nil); err != nil {
		return fmt.Errorf("stage GroupAdmin removal: %w", err)
	}
	return nil
}

// RemoveGroupAdmin opens an atomic batch and applies StageRemoveGroupAdmin.
func RemoveGroupAdmin(db *store.Store, group, target uuid.UUID) error {
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		return StageRemoveGroupAdmin(db, batch, group, target)
	})
}
