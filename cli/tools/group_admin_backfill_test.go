package main

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func backfillTestUser(t *testing.T, db *store.Store, id string) uuid.UUID {
	t.Helper()
	user := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{Uuid: user.String(), Id: id, Type: "user"}))
	return user
}

func backfillTestGroup(t *testing.T, db *store.Store, id string, private bool) uuid.UUID {
	t.Helper()
	group := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{
		Uuid: group.String(), Id: id, Type: "group", Private: private,
	}))
	return group
}

func backfillLegacyAdmins(t *testing.T, db *store.Store, group uuid.UUID, snapshots ...*pb.Profile) {
	t.Helper()
	require.NoError(t, model.PutFeedinfo(db, group.String(), &pb.Feedinfo{
		Uuid:   group.String(),
		Admins: snapshots,
	}))
}

func TestBackfillGroupAdmins(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	alice := backfillTestUser(t, db, "alice")
	bobby := backfillTestUser(t, db, "bobby")
	// bobby was renamed from bob: the legacy snapshot still names the old ID.
	require.NoError(t, db.Set(
		model.NewKeyFrom(model.UserRenameMap.Prefix, []byte("bob")), bobby.Bytes()))
	carol := backfillTestUser(t, db, "carol")
	carolProfile, err := model.GetProfileFromUuid(db, carol)
	require.NoError(t, err)
	carolProfile.Deleted = true
	_, err = model.Profile.Put(db, carol.Bytes(), carolProfile)
	require.NoError(t, err)
	// "dave" never resolves: neither a current ID nor a rename-map entry.

	// g1: private legacy Group; snapshots cover a current ID, a renamed ID,
	// and a duplicate that must be folded.
	g1 := backfillTestGroup(t, db, "g1-private", true)
	backfillLegacyAdmins(t, db, g1,
		&pb.Profile{Id: "alice"}, &pb.Profile{Id: "bob"}, &pb.Profile{Id: "alice"})
	// g2: every snapshot is deleted or missing, so the Group stays orphaned.
	g2 := backfillTestGroup(t, db, "g2-public", false)
	backfillLegacyAdmins(t, db, g2, &pb.Profile{Id: "carol"}, &pb.Profile{Id: "dave"})
	// g3: already has an admin; its legacy snapshots must not add more.
	g3 := backfillTestGroup(t, db, "g3-managed", false)
	g3AdminKey, err := model.GroupAdminKey(g3, alice)
	require.NoError(t, err)
	require.NoError(t, db.Set(g3AdminKey, nil))
	backfillLegacyAdmins(t, db, g3, &pb.Profile{Id: "bobby"})
	// g4: no legacy feedinfo at all.
	g4 := backfillTestGroup(t, db, "g4-nofeedinfo", false)
	// g5: snapshot carries only the stable UUID, no ID.
	g5 := backfillTestGroup(t, db, "g5-uuid-snapshot", true)
	backfillLegacyAdmins(t, db, g5, &pb.Profile{Uuid: alice.String()})

	expectStats := func(stats groupAdminBackfillStats, alreadyAdmined, backfilled, adminsWritten, unresolved, orphaned int) {
		t.Helper()
		require.Equal(t, 5, stats.groupsScanned)
		require.Equal(t, alreadyAdmined, stats.alreadyAdmined)
		require.Equal(t, backfilled, stats.backfilled)
		require.Equal(t, adminsWritten, stats.adminsWritten)
		require.Equal(t, unresolved, stats.unresolved)
		require.Equal(t, orphaned, stats.orphaned)
	}

	dryStats, err := backfillGroupAdmins(db, true)
	require.NoError(t, err)
	expectStats(dryStats, 1, 2, 3, 2, 2)
	count, err := model.CountGroupAdmins(db, g1)
	require.NoError(t, err)
	require.Equal(t, 0, count, "dry-run must not write admin rows")

	stats, err := backfillGroupAdmins(db, false)
	require.NoError(t, err)
	expectStats(stats, 1, 2, 3, 2, 2)

	isAdmin, err := model.IsGroupAdmin(db, g1, alice)
	require.NoError(t, err)
	require.True(t, isAdmin)
	isAdmin, err = model.IsGroupAdmin(db, g1, bobby)
	require.NoError(t, err)
	require.True(t, isAdmin, "renamed account must resolve through UserRenameMap")
	isMember, err := model.IsGroupMember(db, g1, alice)
	require.NoError(t, err)
	require.True(t, isMember, "backfilled admin must also hold the membership edge")
	isMember, err = model.IsGroupMember(db, g1, bobby)
	require.NoError(t, err)
	require.True(t, isMember)

	isAdmin, err = model.IsGroupAdmin(db, g5, alice)
	require.NoError(t, err)
	require.True(t, isAdmin, "UUID-only snapshot must resolve")

	for _, group := range []uuid.UUID{g2, g4} {
		count, err := model.CountGroupAdmins(db, group)
		require.NoError(t, err)
		require.Equal(t, 0, count, "unresolvable snapshots must not mint admins")
	}
	count, err = model.CountGroupAdmins(db, g3)
	require.NoError(t, err)
	require.Equal(t, 1, count, "a Group that already has an admin must be left untouched")
	isAdmin, err = model.IsGroupAdmin(db, g3, bobby)
	require.NoError(t, err)
	require.False(t, isAdmin)

	// The backfill is idempotent: a second run sees every Group as handled.
	stats, err = backfillGroupAdmins(db, false)
	require.NoError(t, err)
	expectStats(stats, 3, 0, 0, 2, 2)
}
