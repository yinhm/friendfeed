package server

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func TestCreateGroupAtomicMembershipAndAdmin(t *testing.T) {
	srv := newServiceServer(t)
	actor := createServiceUser(t, srv, "creator")

	resp, err := srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid:   actor.String(),
		Id:          "book-club",
		Name:        "Book Club",
		Description: "reading group",
	})
	require.NoError(t, err)
	require.Equal(t, "group", resp.Type)
	require.Equal(t, "book-club", resp.Id)

	groupUUID, err := uuid.FromString(resp.Uuid)
	require.NoError(t, err)

	isAdmin, err := model.IsGroupAdmin(srv.rdb, groupUUID, actor)
	require.NoError(t, err)
	require.True(t, isAdmin)

	followed, err := srv.rdb.Exists(model.NewKeyFrom(model.Follow.Prefix, actor.Bytes(), groupUUID.Bytes()))
	require.NoError(t, err)
	require.True(t, followed)

	followerExists, err := srv.rdb.Exists(model.NewKeyFrom(model.Follower.Prefix, groupUUID.Bytes(), actor.Bytes()))
	require.NoError(t, err)
	require.True(t, followerExists)

	fetched, err := model.GetProfileFromUserId(srv.rdb, "book-club")
	require.NoError(t, err)
	require.Equal(t, resp.Uuid, fetched.Uuid)
}

func TestCreateGroupRejectsPrivate(t *testing.T) {
	srv := newServiceServer(t)
	actor := createServiceUser(t, srv, "creator2")

	_, err := srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid: actor.String(),
		Id:        "secret-club",
		Name:      "Secret Club",
		Private:   true,
	})
	require.Error(t, err)
}

func TestCreateGroupRejectsDuplicateID(t *testing.T) {
	srv := newServiceServer(t)
	actor := createServiceUser(t, srv, "creator3")

	_, err := srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid: actor.String(),
		Id:        "dup-club",
		Name:      "Dup Club",
	})
	require.NoError(t, err)

	other := createServiceUser(t, srv, "creator4")
	_, err = srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid: other.String(),
		Id:        "dup-club",
		Name:      "Dup Club Again",
	})
	require.Error(t, err)
}

func TestCreateGroupRejectsNonUserActor(t *testing.T) {
	srv := newServiceServer(t)
	group := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{Uuid: group.String(), Id: "existing-group", Type: "group"}))

	_, err := srv.CreateGroup(context.Background(), &pb.CreateGroupRequest{
		ActorUuid: group.String(),
		Id:        "nested-club",
		Name:      "Nested Club",
	})
	require.Error(t, err)
}
