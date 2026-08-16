package model

import (
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
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
