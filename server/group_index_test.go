package server

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func TestListGroupsPagesActivityIndexWithoutRelationshipState(t *testing.T) {
	srv := newServiceServer(t)
	creator := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{Uuid: creator.String(), Id: "directory-owner", Type: "user"}))
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	var profiles []*pb.Profile
	for i, id := range []string{"old-group", "middle-group", "new-group"} {
		profile, err := model.CreateGroup(srv.rdb, creator, id, id, "description", "", i == 1, base.Add(time.Duration(i)*time.Minute))
		require.NoError(t, err)
		profiles = append(profiles, profile)
	}

	first, err := srv.ListGroups(context.Background(), &pb.ListGroupsRequest{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []string{profiles[2].Uuid, profiles[1].Uuid}, []string{first.Groups[0].Uuid, first.Groups[1].Uuid})
	require.True(t, first.Groups[1].Private)
	require.NotEmpty(t, first.NextCursor)

	second, err := srv.ListGroups(context.Background(), &pb.ListGroupsRequest{Limit: 2, Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Groups, 1)
	require.Equal(t, profiles[0].Uuid, second.Groups[0].Uuid)
	require.Empty(t, second.NextCursor)
}

func TestListGroupsSkipsOrphansAndRejectsInvalidCursor(t *testing.T) {
	srv := newServiceServer(t)
	orphan := uuid.Must(uuid.NewV4())
	require.NoError(t, srv.rdb.ApplyBatch(func(batch *pebble.Batch) error {
		return model.StageCreateGroupIndex(batch, orphan, time.Now().UTC())
	}))

	response, err := srv.ListGroups(context.Background(), &pb.ListGroupsRequest{})
	require.NoError(t, err)
	require.Empty(t, response.Groups)
	require.Empty(t, response.NextCursor)

	_, err = srv.ListGroups(context.Background(), &pb.ListGroupsRequest{Cursor: "not-a-cursor"})
	require.Error(t, err)
}
