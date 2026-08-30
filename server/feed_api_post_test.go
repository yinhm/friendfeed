package server

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func createServiceGroup(t *testing.T, srv *ApiServer, id string) uuid.UUID {
	t.Helper()
	group := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{
		Uuid: group.String(), Id: id, Name: "API Group", Type: "group", Private: true,
	}))
	return group
}

func TestFeedPrincipalPostEntryIsCreateOnlyAndServerAuthored(t *testing.T) {
	srv := newServiceServer(t)
	group := createServiceGroup(t, srv, "api-machine-group")
	ctx := incomingFeedPrincipal(t, group.String())
	forgedID := uuid.Must(uuid.NewV4()).String()
	request := &pb.Entry{
		Id: forgedID, Date: "1999-01-01T00:00:00Z", ProfileUuid: uuid.Must(uuid.NewV4()).String(),
		FeedUuid: uuid.Must(uuid.NewV4()).String(), Body: "machine body", RawBody: "secret editor state",
		From: &pb.Feed{Id: "forged"}, To: []*pb.Feed{{Id: "forged"}}, Via: &pb.Via{Name: "forged"},
		Commands: []string{"edit"}, Likes: []*pb.Like{{}}, Comments: []*pb.Comment{{}},
	}

	first, err := srv.PostEntry(ctx, request)
	require.NoError(t, err)
	second, err := srv.PostEntry(ctx, request)
	require.NoError(t, err)
	require.NotEqual(t, forgedID, first.Id)
	require.NotEqual(t, first.Id, second.Id, "V1 POST is intentionally non-idempotent")
	require.Equal(t, group.String(), first.ProfileUuid)
	require.Equal(t, group.String(), first.FeedUuid)
	require.Equal(t, "api-machine-group", first.From.Id)
	require.Equal(t, "group", first.From.Type)
	require.Empty(t, first.To)
	require.Equal(t, "FriendFeed API", first.Via.Name)
	require.Empty(t, first.Via.Url)
	require.Empty(t, first.RawBody)
	require.Empty(t, first.Commands)
	require.Empty(t, first.Likes)
	require.Empty(t, first.Comments)
	parsed, err := time.Parse(time.RFC3339Nano, first.Date)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().UTC(), parsed, time.Second)

	_, err = model.GetEntry(srv.rdb, forgedID)
	require.Error(t, err, "caller-supplied ID must never be created or overwritten")
	feed, err := srv.FetchFeed(ctx, &pb.FeedRequest{ProfileUuid: group.String(), CursorPaging: true, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, feed.Entries, 2)
}

func TestPostEntryWithoutFeedPrincipalCannotImpersonateGroup(t *testing.T) {
	srv := newServiceServer(t)
	group := createServiceGroup(t, srv, "ordinary-group")
	_, err := srv.PostEntry(context.Background(), &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), Date: time.Now().UTC().Format(time.RFC3339Nano),
		ProfileUuid: group.String(), FeedUuid: group.String(), Body: "forged group author",
	})
	require.Error(t, err)
}
