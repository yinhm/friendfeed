package server

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func importTwitterRequest(item string) *pb.ImportFeedEntryRequest {
	return &pb.ImportFeedEntryRequest{
		SourceKind: "twitter", SourceAccountId: "12345", SourceItemId: item,
		SourceUrl:   "https://x.com/example/status/" + item,
		PublishedAt: "2020-08-17T12:34:56Z", BodyHtml: "historical tweet",
	}
}

func TestImportFeedEntryCreatesOnceWithoutRealtimeTimelines(t *testing.T) {
	srv := newPublicTestServer(t)
	index, err := search.OpenIndex(t.TempDir())
	require.NoError(t, err)
	mockIndexPath := filepath.Join(t.TempDir(), "index")
	search.Indexer = index
	t.Cleanup(func() {
		require.NoError(t, index.Close())
		search.InitMockIndexService(mockIndexPath)
	})
	profile := addPublicTestProfile(t, srv, "import-owner", false)
	ctx := incomingFeedPrincipal(t, profile.Uuid)
	request := importTwitterRequest("1295071681511407617")

	first, err := srv.ImportFeedEntry(ctx, request)
	require.NoError(t, err)
	require.True(t, first.Created)
	require.Equal(t, model.UniqueKeyFrom("external-entry", profile.Uuid, "twitter", request.SourceItemId).String(), first.Entry.Id)
	require.Equal(t, profile.Uuid, first.Entry.ProfileUuid)
	require.Equal(t, profile.Uuid, first.Entry.FeedUuid)
	require.Equal(t, request.PublishedAt, first.Entry.Date)
	require.Nil(t, first.Entry.To)

	second, err := srv.ImportFeedEntry(ctx, request)
	require.NoError(t, err)
	require.False(t, second.Created)
	require.Equal(t, first.Entry.Id, second.Entry.Id)

	entryID := uuid.FromStringOrNil(first.Entry.Id)
	_, err = model.TimelinePositionTime(srv.rdb, uuid.FromStringOrNil(profile.Uuid), entryID)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = model.TimelinePositionTime(srv.rdb, model.PublicTimelineUUID, entryID)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = model.FeedArchiveDirtySince(srv.rdb, uuid.FromStringOrNil(profile.Uuid))
	require.NoError(t, err)
	searchRequest := bleve.NewSearchRequest(bleve.NewQueryStringQuery("historical"))
	searchResult, err := index.Search(searchRequest)
	require.NoError(t, err)
	require.Len(t, searchResult.Hits, 1)
	require.Equal(t, first.Entry.Id, searchResult.Hits[0].ID)
}

func TestImportFeedEntryReplaysLegacyTwitterUUID(t *testing.T) {
	srv := newPublicTestServer(t)
	profile := addPublicTestProfile(t, srv, "legacy-owner", false)
	item := "1295071681511407618"
	legacyID := model.UniqueKeyFrom("twitter", item)
	legacy := &pb.Entry{
		Id: legacyID.String(), Url: "http://friendfeed.com/legacy/entry",
		RawLink: "https://twitter.com/legacy/statuses/" + item, Date: "2020-01-01T00:00:00Z",
		Body: "legacy", ProfileUuid: profile.Uuid, FeedUuid: profile.Uuid,
	}
	_, err := model.PutArchiveEntry(srv.rdb, legacy)
	require.NoError(t, err)

	response, err := srv.ImportFeedEntry(incomingFeedPrincipal(t, profile.Uuid), importTwitterRequest(item))
	require.NoError(t, err)
	require.False(t, response.Created)
	require.True(t, response.LegacyReplay)
	require.Equal(t, legacyID.String(), response.Entry.Id)
}

func TestImportFeedEntryConcurrentReplayHasSingleCreate(t *testing.T) {
	srv := newPublicTestServer(t)
	profile := addPublicTestProfile(t, srv, "concurrent-import", false)
	ctx := incomingFeedPrincipal(t, profile.Uuid)
	request := importTwitterRequest("1295071681511407619")

	var wg sync.WaitGroup
	created := make(chan bool, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := srv.ImportFeedEntry(ctx, request)
			if err != nil {
				errs <- err
				return
			}
			created <- response.Created
		}()
	}
	wg.Wait()
	close(created)
	close(errs)
	require.Empty(t, errs)
	count := 0
	for value := range created {
		if value {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestImportFeedEntryRejectsMissingPrincipalAndIdentityConflict(t *testing.T) {
	srv := newPublicTestServer(t)
	_, err := srv.ImportFeedEntry(context.Background(), importTwitterRequest("1295071681511407620"))
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	owner := addPublicTestProfile(t, srv, "conflict-owner", false)
	request := importTwitterRequest("1295071681511407621")
	newID := model.UniqueKeyFrom("external-entry", owner.Uuid, "twitter", request.SourceItemId)
	_, err = model.PutArchiveEntry(srv.rdb, &pb.Entry{
		Id: newID.String(), Date: time.Now().UTC().Format(time.RFC3339Nano), Body: "collision",
		Url: "https://x.com/example/status/999", ProfileUuid: owner.Uuid, FeedUuid: owner.Uuid,
	})
	require.NoError(t, err)
	_, err = srv.ImportFeedEntry(incomingFeedPrincipal(t, owner.Uuid), request)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestImportFeedEntryRejectsDeletedFeedAsNotFound(t *testing.T) {
	srv := newPublicTestServer(t)
	profile := addPublicTestProfile(t, srv, "deleted-import-owner", false)
	profile.Deleted = true
	require.NoError(t, model.UpdateProfile(srv.rdb, profile))

	_, err := srv.ImportFeedEntry(
		incomingFeedPrincipal(t, profile.Uuid),
		importTwitterRequest("1295071681511407622"),
	)
	require.Equal(t, codes.NotFound, status.Code(err))
}
