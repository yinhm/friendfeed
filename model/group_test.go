package model

import (
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestGroupAdminKey(t *testing.T) {
	group := uuid.Must(uuid.NewV4())
	admin := uuid.Must(uuid.NewV4())
	key, err := GroupAdminKey(group, admin)
	require.NoError(t, err)
	require.Equal(t, append(append(KeyPrefixToBytes(TableGroupAdmin), group.Bytes()...), admin.Bytes()...), []byte(key))

	_, err = GroupAdminKey(uuid.Nil, admin)
	require.Error(t, err)
	_, err = GroupAdminKey(group, uuid.Nil)
	require.Error(t, err)
}

func TestIsGroupAdminAndListGroupAdmins(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group := uuid.Must(uuid.NewV4())
	admin1 := uuid.Must(uuid.NewV4())
	admin2 := uuid.Must(uuid.NewV4())
	nonAdmin := uuid.Must(uuid.NewV4())

	ok, err := IsGroupAdmin(db, group, admin1)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, db.ApplyBatch(func(batch *pebble.Batch) error {
		for _, admin := range []uuid.UUID{admin1, admin2} {
			key, err := GroupAdminKey(group, admin)
			if err != nil {
				return err
			}
			if err := batch.Set(key, nil, nil); err != nil {
				return err
			}
		}
		return nil
	}))

	ok, err = IsGroupAdmin(db, group, admin1)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = IsGroupAdmin(db, group, nonAdmin)
	require.NoError(t, err)
	require.False(t, ok)

	admins, err := ListGroupAdmins(db, group)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{admin1, admin2}, admins)

	count, err := CountGroupAdmins(db, group)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	_, err = ListGroupAdmins(db, uuid.Nil)
	require.Error(t, err)
}

func TestGroupActionAllowedMatrix(t *testing.T) {
	nonMember := GroupRole{}
	member := GroupRole{IsMember: true}
	admin := GroupRole{IsMember: true, IsAdmin: true}
	super := GroupRole{IsSuper: true}

	cases := []struct {
		name   string
		role   GroupRole
		action GroupAction
		want   bool
	}{
		{"non-member view", nonMember, GroupActionView, true},
		{"non-member join", nonMember, GroupActionJoin, true},
		{"non-member post", nonMember, GroupActionPost, false},
		{"non-member manage members", nonMember, GroupActionManageMembers, false},
		{"non-member manage service", nonMember, GroupActionManageService, false},
		{"non-member manage group", nonMember, GroupActionManageGroup, false},

		{"member view", member, GroupActionView, true},
		{"member join", member, GroupActionJoin, true},
		{"member post", member, GroupActionPost, true},
		{"member manage members", member, GroupActionManageMembers, false},
		{"member manage service", member, GroupActionManageService, false},
		{"member manage group", member, GroupActionManageGroup, false},

		{"admin view", admin, GroupActionView, true},
		{"admin join", admin, GroupActionJoin, true},
		{"admin post", admin, GroupActionPost, true},
		{"admin manage members", admin, GroupActionManageMembers, true},
		{"admin manage service", admin, GroupActionManageService, true},
		{"admin manage group", admin, GroupActionManageGroup, true},

		{"super view", super, GroupActionView, true},
		{"super join", super, GroupActionJoin, true},
		{"super post", super, GroupActionPost, true},
		{"super manage members", super, GroupActionManageMembers, true},
		{"super manage service", super, GroupActionManageService, true},
		{"super manage group", super, GroupActionManageGroup, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, GroupActionAllowed(tc.role, tc.action))
		})
	}
}

func TestCreateGroupRejectsMissingActor(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	actor := uuid.Must(uuid.NewV4())
	_, err = CreateGroup(db, actor, "ghost-club", "Ghost Club", "", "", false, time.Now())
	require.Error(t, err)

	// No residue: the ID must remain unclaimed.
	_, err = db.Get(NewKeyFrom(TableUserMap.Bytes(), []byte("ghost-club")))
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestCreateGroupRejectsNonUserActor(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group := uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{Uuid: group.String(), Id: "existing-group", Type: "group"}))

	_, err = CreateGroup(db, group, "nested-club", "Nested Club", "", "", false, time.Now())
	require.Error(t, err)
}

func TestCreateGroupRejectsPrivate(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	actor := uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{Uuid: actor.String(), Id: "creator", Type: "user"}))

	_, err = CreateGroup(db, actor, "private-club", "Private Club", "", "", true, time.Now())
	require.ErrorIs(t, err, ErrPrivateGroupUnsupported)

	_, err = db.Get(NewKeyFrom(TableUserMap.Bytes(), []byte("private-club")))
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestCreateGroupAtomicWrites(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	actor := uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{Uuid: actor.String(), Id: "creator", Type: "user"}))

	now := time.Now()
	group, err := CreateGroup(db, actor, "Book-Club", "Book Club", "for readers", "pic.png", false, now)
	require.NoError(t, err)
	require.Equal(t, "group", group.Type)
	require.Equal(t, "book-club", group.Id) // normalized lowercase

	groupUUID, err := uuid.FromString(group.Uuid)
	require.NoError(t, err)

	isAdmin, err := IsGroupAdmin(db, groupUUID, actor)
	require.NoError(t, err)
	require.True(t, isAdmin)

	followed, err := db.Exists(NewKeyFrom(Follow.Prefix, actor.Bytes(), groupUUID.Bytes()))
	require.NoError(t, err)
	require.True(t, followed)

	followerExists, err := db.Exists(NewKeyFrom(Follower.Prefix, groupUUID.Bytes(), actor.Bytes()))
	require.NoError(t, err)
	require.True(t, followerExists)

	// Duplicate ID must be rejected without disturbing the existing Group.
	other := uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{Uuid: other.String(), Id: "other", Type: "user"}))
	_, err = CreateGroup(db, other, "book-club", "Book Club Dup", "", "", false, now)
	require.Error(t, err)

	adminCount, err := CountGroupAdmins(db, groupUUID)
	require.NoError(t, err)
	require.Equal(t, 1, adminCount)
}

func setupGroupWithCreator(t *testing.T, db *store.Store) (group uuid.UUID, creator uuid.UUID) {
	t.Helper()
	creator = uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{Uuid: creator.String(), Id: "creator", Type: "user"}))
	profile, err := CreateGroup(db, creator, "club", "Club", "", "", false, time.Now())
	require.NoError(t, err)
	group, err = uuid.FromString(profile.Uuid)
	require.NoError(t, err)
	return group, creator
}

func newGroupUser(t *testing.T, db *store.Store, id string) uuid.UUID {
	t.Helper()
	user := uuid.Must(uuid.NewV4())
	require.NoError(t, UpdateProfile(db, &pb.Profile{Uuid: user.String(), Id: id, Type: "user"}))
	return user
}

func TestJoinGroupIsIdempotent(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group, _ := setupGroupWithCreator(t, db)
	member := newGroupUser(t, db, "member1")

	require.NoError(t, JoinGroup(db, group, member))
	isMember, err := IsGroupMember(db, group, member)
	require.NoError(t, err)
	require.True(t, isMember)

	// Idempotent: joining again succeeds without error.
	require.NoError(t, JoinGroup(db, group, member))
}

func TestJoinGroupRejectsPrivate(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group, _ := setupGroupWithCreator(t, db)
	groupProfile, err := GetProfileFromUuid(db, group)
	require.NoError(t, err)
	groupProfile.Private = true
	require.NoError(t, UpdateProfile(db, groupProfile))

	member := newGroupUser(t, db, "member1")
	err = JoinGroup(db, group, member)
	require.ErrorIs(t, err, ErrPrivateGroupUnsupported)
}

func TestLeaveGroupOrdinaryMember(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group, _ := setupGroupWithCreator(t, db)
	member := newGroupUser(t, db, "member1")
	require.NoError(t, JoinGroup(db, group, member))

	require.NoError(t, LeaveGroup(db, group, member))
	isMember, err := IsGroupMember(db, group, member)
	require.NoError(t, err)
	require.False(t, isMember)

	// Idempotent: leaving again succeeds without error.
	require.NoError(t, LeaveGroup(db, group, member))
}

func TestLeaveGroupRejectsAdminWithoutDemotion(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group, creator := setupGroupWithCreator(t, db)
	err = LeaveGroup(db, group, creator)
	require.ErrorIs(t, err, ErrGroupAdminMustBeDemotedFirst)

	isMember, err := IsGroupMember(db, group, creator)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestAddGroupAdminRequiresPriorMembership(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group, _ := setupGroupWithCreator(t, db)
	nonMember := newGroupUser(t, db, "nonmember")

	err = AddGroupAdmin(db, group, nonMember)
	require.Error(t, err)

	require.NoError(t, JoinGroup(db, group, nonMember))
	require.NoError(t, AddGroupAdmin(db, group, nonMember))

	isAdmin, err := IsGroupAdmin(db, group, nonMember)
	require.NoError(t, err)
	require.True(t, isAdmin)
}

func TestRemoveGroupAdminRejectsLastAdmin(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group, creator := setupGroupWithCreator(t, db)
	err = RemoveGroupAdmin(db, group, creator)
	require.ErrorIs(t, err, ErrLastGroupAdmin)

	isAdmin, err := IsGroupAdmin(db, group, creator)
	require.NoError(t, err)
	require.True(t, isAdmin)
}

func TestRemoveGroupAdminSucceedsWithAnotherAdmin(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group, creator := setupGroupWithCreator(t, db)
	other := newGroupUser(t, db, "other")
	require.NoError(t, JoinGroup(db, group, other))
	require.NoError(t, AddGroupAdmin(db, group, other))

	require.NoError(t, RemoveGroupAdmin(db, group, creator))

	isAdmin, err := IsGroupAdmin(db, group, creator)
	require.NoError(t, err)
	require.False(t, isAdmin)

	// creator is still a plain member and can now leave.
	require.NoError(t, LeaveGroup(db, group, creator))
}

func TestRemoveGroupMemberRejectsCurrentAdmin(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group, creator := setupGroupWithCreator(t, db)
	err = RemoveGroupMember(db, group, creator)
	require.ErrorIs(t, err, ErrGroupAdminMustBeDemotedFirst)
}

func TestRemoveGroupMemberOrdinaryMember(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group, _ := setupGroupWithCreator(t, db)
	member := newGroupUser(t, db, "member1")
	require.NoError(t, JoinGroup(db, group, member))

	require.NoError(t, RemoveGroupMember(db, group, member))
	isMember, err := IsGroupMember(db, group, member)
	require.NoError(t, err)
	require.False(t, isMember)
}
