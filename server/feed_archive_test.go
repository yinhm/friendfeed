package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/util"
)

func newFeedArchiveTestServer(t *testing.T) *ApiServer {
	t.Helper()
	root := t.TempDir()
	search.InitMockIndexService(filepath.Join(root, "index"))
	srv, err := NewApiServer(filepath.Join(root, "db"), &util.Config{MediaPath: filepath.Join(root, "media")})
	require.NoError(t, err)
	t.Cleanup(func() { srv.Shutdown() })
	return srv
}

func TestAuthenticatedFeedReadStagesAndConsumesArchiveRebuild(t *testing.T) {
	srv := newFeedArchiveTestServer(t)
	viewer := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{Uuid: viewer.String(), Id: "archive-user", Name: "Archive User", Type: "user"}
	require.NoError(t, model.UpdateProfile(srv.rdb, profile))
	oldestID := uuid.Must(uuid.NewV4()).String()
	newest2025ID := uuid.Must(uuid.NewV4()).String()
	_, err := srv.PostEntry(context.Background(), &pb.Entry{
		Id: oldestID, Date: time.Date(2025, 4, 3, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		ProfileUuid: viewer.String(), FeedUuid: viewer.String(), Body: "old",
	})
	require.NoError(t, err)
	_, err = srv.PostEntry(context.Background(), &pb.Entry{
		Id: newest2025ID, Date: time.Date(2025, 12, 3, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		ProfileUuid: viewer.String(), FeedUuid: viewer.String(), Body: "newer",
	})
	require.NoError(t, err)
	_, err = srv.PostEntry(context.Background(), &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), Date: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		ProfileUuid: viewer.String(), FeedUuid: viewer.String(), Body: "newest",
	})
	require.NoError(t, err)

	request := &pb.FeedRequest{Id: profile.Id, ViewerUuid: viewer.String(), CursorPaging: true, PageSize: 30}
	feed, err := srv.FetchFeed(context.Background(), request)
	require.NoError(t, err)
	require.Nil(t, feed.Archive)
	_, err = srv.FetchFeed(context.Background(), request)
	require.NoError(t, err)

	claimed, err := srv.tasks.Claim(context.Background(), "archive-test", []string{feedArchiveRebuildTaskType}, 2)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, srv.handleFeedArchiveTask(context.Background(), claimed[0]))
	require.NoError(t, srv.tasks.Complete(context.Background(), "archive-test", claimed[0].Id, claimed[0].LeaseEpoch))

	feed, err = srv.FetchFeed(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, feed.Archive)
	require.Equal(t, int64(3), feed.Archive.EntryCount)
	require.Len(t, feed.Archive.Years, 2)
	require.Equal(t, int32(2026), feed.Archive.Years[0].Year)
	// The newest year has no boundary; its link is the Feed's first page.
	require.Empty(t, feed.Archive.Years[0].Cursor)
	require.Equal(t, int32(2025), feed.Archive.Years[1].Year)
	require.NotEmpty(t, feed.Archive.Years[1].Cursor)

	// Following a year cursor opens the year at its newest Entry.
	yearPage, err := srv.FetchFeed(context.Background(), &pb.FeedRequest{
		Id: profile.Id, ViewerUuid: viewer.String(), CursorPaging: true,
		Cursor: feed.Archive.Years[1].Cursor, PageSize: 30,
	})
	require.NoError(t, err)
	require.NotEmpty(t, yearPage.Entries)
	require.Equal(t, newest2025ID, yearPage.Entries[0].Id)

	// A real Entry creation preserves the stale snapshot and records the first
	// dirty time. The normal one-week delay does not hide the Archive or stage
	// immediate maintenance.
	_, err = srv.PostEntry(context.Background(), &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), Date: time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		ProfileUuid: viewer.String(), FeedUuid: viewer.String(), Body: "new",
	})
	require.NoError(t, err)
	archive, err := model.GetFeedArchive(srv.rdb, viewer)
	require.NoError(t, err)
	require.Equal(t, int64(3), archive.EntryCount)
	_, err = model.FeedArchiveDirtySince(srv.rdb, viewer)
	require.NoError(t, err)
	feed, err = srv.FetchFeed(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, feed.Archive)
	claimed, err = srv.tasks.Claim(context.Background(), "archive-test", []string{feedArchiveRebuildTaskType}, 1)
	require.NoError(t, err)
	require.Empty(t, claimed)
}

func TestAnonymousFeedReadDoesNotExposeOrStageArchive(t *testing.T) {
	srv := newFeedArchiveTestServer(t)
	owner := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(srv.rdb, &pb.Profile{Uuid: owner.String(), Id: "public-owner", Type: "user"}))
	require.NoError(t, model.PutFeedArchive(srv.rdb, owner, &pb.FeedArchiveStats{EntryCount: 7}))

	feed, err := srv.FetchFeed(context.Background(), &pb.FeedRequest{Id: "public-owner", CursorPaging: true, PageSize: 30})
	require.NoError(t, err)
	require.Nil(t, feed.Archive)
	claimed, err := srv.tasks.Claim(context.Background(), "archive-test", []string{feedArchiveRebuildTaskType}, 1)
	require.NoError(t, err)
	require.Empty(t, claimed)
}
