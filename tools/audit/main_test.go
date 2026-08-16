package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestUser(t *testing.T, db *store.Store, id string) uuid.UUID {
	t.Helper()
	user := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{Uuid: user.String(), Id: id, Type: "user"}))
	return user
}

// setupCleanGroup builds a well-formed group: creator (admin + member) and one
// joined member, with paired Follow/Follower edges everywhere.
func setupCleanGroup(t *testing.T, db *store.Store) (creator, member uuid.UUID, group *pb.Profile) {
	t.Helper()
	creator = newTestUser(t, db, "creator")
	member = newTestUser(t, db, "member")
	var err error
	group, err = model.CreateGroup(db, creator, "book-club", "Book Club", "", "", false, time.Now())
	require.NoError(t, err)
	require.NoError(t, model.JoinGroup(db, groupUUID(t, group), member))
	return creator, member, group
}

func groupUUID(t *testing.T, group *pb.Profile) uuid.UUID {
	t.Helper()
	g, err := uuid.FromString(group.Uuid)
	require.NoError(t, err)
	return g
}

func runAllChecks(db *store.Store) map[string]int {
	ctx := context.Background()
	return map[string]int{
		"adminNonMembers":       len(auditAdminNonMembers(ctx, db)),
		"groupsWithoutAdmins":   len(auditGroupsWithoutAdmins(ctx, db)),
		"orphanedMemberships":   len(auditOrphanedMemberships(ctx, db)),
		"unpairedMemberships":   len(auditUnpairedMemberships(ctx, db)),
		"deletedGroupResiduals": len(auditDeletedGroupResiduals(ctx, db)),
	}
}

func TestAuditCleanDatabase(t *testing.T) {
	db := newTestStore(t)
	setupCleanGroup(t, db)

	for name, count := range runAllChecks(db) {
		require.Equal(t, 0, count, "check %s reported issues on a clean database", name)
	}
}

func TestAuditAdminNonMember(t *testing.T) {
	db := newTestStore(t)
	_, _, group := setupCleanGroup(t, db)
	g := groupUUID(t, group)

	// Write a GroupAdmin row for a user who has no Follow edge.
	stranger := newTestUser(t, db, "stranger")
	key, err := model.GroupAdminKey(g, stranger)
	require.NoError(t, err)
	require.NoError(t, db.Set(key, nil))

	issues := auditAdminNonMembers(context.Background(), db)
	require.Len(t, issues, 1)
	require.Contains(t, issues[0], stranger.String())
	require.Contains(t, issues[0], g.String())

	// Other checks stay silent about this inconsistency.
	require.Equal(t, 0, len(auditUnpairedMemberships(context.Background(), db)))
}

func TestAuditGroupWithoutAdmins(t *testing.T) {
	db := newTestStore(t)

	// A bare group profile with no GroupAdmin rows at all.
	bare := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{Uuid: bare.String(), Id: "bare-group", Name: "Bare", Type: "group"}))

	issues := auditGroupsWithoutAdmins(context.Background(), db)
	require.Len(t, issues, 1)
	require.Contains(t, issues[0], bare.String())
}

func TestAuditFollowEdgeToDeletedGroup(t *testing.T) {
	db := newTestStore(t)
	_, _, group := setupCleanGroup(t, db)
	g := groupUUID(t, group)

	// Soft-delete the group profile in place: Deleted=true via a raw Put.
	profile, err := model.GetProfileFromUuid(db, g)
	require.NoError(t, err)
	profile.Deleted = true
	_, err = model.Profile.Put(db, g.Bytes(), profile)
	require.NoError(t, err)

	// Both membership Follow edges now point at a deleted group.
	issues := auditOrphanedMemberships(context.Background(), db)
	require.Len(t, issues, 2)
	for _, issue := range issues {
		require.Contains(t, issue, g.String())
	}

	// The creator's GroupAdmin row is now a deleted-group residual too.
	residuals := auditDeletedGroupResiduals(context.Background(), db)
	require.Len(t, residuals, 1)
	require.Contains(t, residuals[0], g.String())
}

func TestAuditUnpairedMembership(t *testing.T) {
	db := newTestStore(t)
	_, _, group := setupCleanGroup(t, db)
	g := groupUUID(t, group)

	// Follow without Follower.
	lurker := newTestUser(t, db, "lurker")
	require.NoError(t, db.Set(model.NewKeyFrom(model.Follow.Prefix, lurker.Bytes(), g.Bytes()), []byte("1")))

	// Follower without Follow.
	ghost := newTestUser(t, db, "ghost")
	require.NoError(t, db.Set(model.NewKeyFrom(model.Follower.Prefix, g.Bytes(), ghost.Bytes()), []byte("1")))

	issues := auditUnpairedMemberships(context.Background(), db)
	require.Len(t, issues, 2)

	var followIssue, followerIssue string
	for _, issue := range issues {
		if strings.Contains(issue, lurker.String()) {
			followIssue = issue
		}
		if strings.Contains(issue, ghost.String()) {
			followerIssue = issue
		}
	}
	require.Contains(t, followIssue, "no matching Follower edge")
	require.Contains(t, followerIssue, "no matching Follow edge")
}
