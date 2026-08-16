package server

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFetchInteractionFeedIsOwnerOnlyAndPages(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "interaction-owner")
	other := createServiceUser(t, srv, "interaction-other")
	profile, err := model.GetProfileFromUuid(srv.rdb, owner)
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		entry := &pb.Entry{
			Id:          uuid.Must(uuid.NewV4()).String(),
			ProfileUuid: owner.String(),
			FeedUuid:    owner.String(),
			Date:        time.Now().UTC().Format(time.RFC3339),
			Body:        "entry",
		}
		_, err := model.PutEntry(srv.rdb, entry)
		require.NoError(t, err)
		_, _, err = model.PutLike(srv.rdb, profile, entry)
		require.NoError(t, err)
	}

	_, err = srv.FetchInteractionFeed(context.Background(), &pb.InteractionFeedRequest{
		ProfileUuid: owner.String(), ViewerUuid: other.String(),
		Kind: pb.InteractionKind_INTERACTION_KIND_LIKE,
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	first, err := srv.FetchInteractionFeed(context.Background(), &pb.InteractionFeedRequest{
		ProfileUuid: owner.String(), ViewerUuid: owner.String(), PageSize: 1,
		Kind: pb.InteractionKind_INTERACTION_KIND_LIKE,
	})
	require.NoError(t, err)
	require.Len(t, first.Items, 1)
	require.NotNil(t, first.Items[0].Like)
	require.NotEmpty(t, first.NextCursor)

	second, err := srv.FetchInteractionFeed(context.Background(), &pb.InteractionFeedRequest{
		ProfileUuid: owner.String(), ViewerUuid: owner.String(), PageSize: 1,
		Kind: pb.InteractionKind_INTERACTION_KIND_LIKE, Cursor: first.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	require.NotEqual(t, first.Items[0].Entry.Id, second.Items[0].Entry.Id)
}

func TestFetchCommentInteractionFeedReturnsLatestCommentOnly(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "comment-owner")
	profile, err := model.GetProfileFromUuid(srv.rdb, owner)
	require.NoError(t, err)
	entry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: owner.String(),
		FeedUuid:    owner.String(),
		Date:        time.Now().UTC().Format(time.RFC3339),
	}
	_, err = model.PutEntry(srv.rdb, entry)
	require.NoError(t, err)
	first := uuid.Must(uuid.NewV4())
	latest := uuid.Must(uuid.NewV4())
	_, entry, err = model.PutComment(srv.rdb, profile, entry, &pb.Comment{Id: first.String(), Body: "first"})
	require.NoError(t, err)
	time.Sleep(2 * time.Millisecond)
	_, _, err = model.PutComment(srv.rdb, profile, entry, &pb.Comment{Id: latest.String(), Body: "latest"})
	require.NoError(t, err)

	response, err := srv.FetchInteractionFeed(context.Background(), &pb.InteractionFeedRequest{
		ProfileUuid: owner.String(), ViewerUuid: owner.String(),
		Kind: pb.InteractionKind_INTERACTION_KIND_COMMENT,
	})
	require.NoError(t, err)
	require.Len(t, response.Items, 1)
	require.Equal(t, latest.String(), response.Items[0].LatestComment.Id)
	require.Len(t, response.Items[0].Entry.Comments, 2)
}
