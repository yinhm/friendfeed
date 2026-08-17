package main

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestRebuildGroupActivityDryRunAndApply(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	user := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{
		Uuid: user.String(), Id: "rebuild-user", Name: "Rebuild", Type: "user",
	}))
	group, err := model.CreateGroup(db, user, "rebuild-group", "Rebuild Group", "", "", false, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, db.Delete(model.GroupActivityMetaKey(user)))

	stats, err := rebuildGroupActivity(db, "rebuild-user", true)
	require.NoError(t, err)
	require.Equal(t, 1, stats.changed)
	_, err = model.GetGroupActivity(db, user)
	require.ErrorIs(t, err, store.ErrNotFound)

	stats, err = rebuildGroupActivity(db, "rebuild-user", false)
	require.NoError(t, err)
	require.Equal(t, 1, stats.changed)
	rows, err := model.GetGroupActivity(db, user)
	require.NoError(t, err)
	require.Equal(t, []model.GroupActivity{{GroupUUID: group.Uuid, Score: model.GroupActivityCreateScore}}, rows)
}

// The default (unscoped) run rebuilds only users with an OAuth login
// identity: ghost/imported profiles and users without any Group membership
// must not get a materialized key at all.
func TestRebuildGroupActivityDefaultsToOAuthUsers(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	newUser := func(id string) uuid.UUID {
		user := uuid.Must(uuid.NewV4())
		require.NoError(t, model.UpdateProfile(db, &pb.Profile{Uuid: user.String(), Id: id, Type: "user"}))
		return user
	}
	active := newUser("active-user")
	ghost := newUser("ghost-user")
	idle := newUser("idle-user")
	_, err = model.PutOAuthUser(db, &pb.OAuthUser{Provider: "twitter", UserId: "active-1", Uuid: active.String()})
	require.NoError(t, err)
	_, err = model.PutOAuthUser(db, &pb.OAuthUser{Provider: "google", UserId: "idle-1", Uuid: idle.String()})
	require.NoError(t, err)

	activeGroup, err := model.CreateGroup(db, active, "active-g", "Active G", "", "", false, time.Now().UTC())
	require.NoError(t, err)
	_, err = model.CreateGroup(db, ghost, "ghost-g", "Ghost G", "", "", false, time.Now().UTC())
	require.NoError(t, err)
	// Clear the rankings staged by CreateGroup so only the rebuild can write them.
	for _, user := range []uuid.UUID{active, ghost} {
		require.NoError(t, db.Delete(model.GroupActivityMetaKey(user)))
	}

	stats, err := rebuildGroupActivity(db, "", false)
	require.NoError(t, err)
	require.Equal(t, 1, stats.users)
	require.Equal(t, 1, stats.changed)
	rows, err := model.GetGroupActivity(db, active)
	require.NoError(t, err)
	require.Equal(t, []model.GroupActivity{{GroupUUID: activeGroup.Uuid, Score: model.GroupActivityCreateScore}}, rows)
	_, err = model.GetGroupActivity(db, ghost)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = model.GetGroupActivity(db, idle)
	require.ErrorIs(t, err, store.ErrNotFound)
}
