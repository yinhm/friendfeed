package server

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/internal/feedprincipal"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/metadata"
)

func incomingFeedPrincipal(t *testing.T, feed string) context.Context {
	t.Helper()
	outgoing, ok := feedprincipal.WithOutgoing(context.Background(), feed, []byte("12345678"))
	require.True(t, ok)
	md, ok := metadata.FromOutgoingContext(outgoing)
	require.True(t, ok)
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestFeedPrincipalReadsOwnPrivateFeedButNotAnotherFeed(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "api-private-owner")
	other := createServiceUser(t, srv, "api-other-owner")
	profile, err := model.GetProfileFromUuid(srv.rdb, owner)
	require.NoError(t, err)
	profile.Private = true
	_, err = model.Profile.Put(srv.rdb, owner.Bytes(), profile)
	require.NoError(t, err)

	posted, err := srv.PostEntry(context.Background(), &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), ProfileUuid: owner.String(), FeedUuid: owner.String(), Body: "private API row", Date: time.Now().UTC().Format(time.RFC3339Nano),
	})
	require.NoError(t, err)

	ctx := incomingFeedPrincipal(t, owner.String())
	feed, err := srv.FetchFeed(ctx, &pb.FeedRequest{
		ProfileUuid: owner.String(), CursorPaging: true, PageSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, feed.Entries, 1)
	entryFeed, err := srv.FetchEntry(ctx, &pb.EntryRequest{Uuid: posted.Id})
	require.NoError(t, err)
	require.Equal(t, owner.String(), entryFeed.Uuid)

	_, err = srv.FetchFeed(ctx, &pb.FeedRequest{
		ProfileUuid: other.String(), CursorPaging: true, PageSize: 10,
	})
	require.Error(t, err)
	otherEntry, err := srv.PostEntry(context.Background(), &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), ProfileUuid: other.String(), FeedUuid: other.String(), Body: "other row", Date: time.Now().UTC().Format(time.RFC3339Nano),
	})
	require.NoError(t, err)
	_, err = srv.FetchEntry(ctx, &pb.EntryRequest{Uuid: otherEntry.Id})
	require.Error(t, err)
}
