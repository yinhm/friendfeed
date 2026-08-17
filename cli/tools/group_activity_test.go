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
