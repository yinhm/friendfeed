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
	"github.com/yinhm/friendfeed/store"
)

func newPublicTestServer(t *testing.T) *ApiServer {
	t.Helper()
	search.InitMockIndexService(filepath.Join(t.TempDir(), "index"))
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return &ApiServer{mdb: db, rdb: db}
}

func addPublicTestProfile(t *testing.T, srv *ApiServer, id string, private bool) *pb.Profile {
	t.Helper()
	profile := &pb.Profile{
		Uuid:    uuid.Must(uuid.NewV4()).String(),
		Id:      id,
		Name:    id,
		Type:    "user",
		Private: private,
	}
	require.NoError(t, model.UpdateProfile(srv.mdb, profile))
	return profile
}

func postPublicTestEntry(t *testing.T, srv *ApiServer, author *pb.Profile, body string) *pb.Entry {
	t.Helper()
	entry, err := srv.PostEntry(context.Background(), &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		Date:        time.Now().UTC().Format(time.RFC3339),
		Body:        body,
		ProfileUuid: author.Uuid,
	})
	require.NoError(t, err)
	return entry
}

func fetchPublicFeed(t *testing.T, srv *ApiServer, req *pb.FeedRequest) *pb.Feed {
	t.Helper()
	if req == nil {
		req = &pb.FeedRequest{Id: "public", PageSize: 30}
	}
	feed, err := srv.FetchFeed(context.Background(), req)
	require.NoError(t, err)
	return feed
}

func publicFeedEntryIDs(feed *pb.Feed) []string {
	ids := make([]string, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		ids = append(ids, entry.Id)
	}
	return ids
}

// publicTimelineActivity returns the public position of an entry, advancing
// the wall clock far enough that a later bump is strictly distinguishable.
func publicTimelineActivity(t *testing.T, srv *ApiServer, entryID string) time.Time {
	t.Helper()
	entryUUID := uuid.Must(uuid.FromString(entryID))
	position, err := model.TimelinePositionTime(srv.rdb, model.PublicTimelineUUID, entryUUID)
	require.NoError(t, err)
	return position
}

func TestPublicFeedCarriesLegacyWireMetadata(t *testing.T) {
	srv := newPublicTestServer(t)
	author := addPublicTestProfile(t, srv, "author", false)
	postPublicTestEntry(t, srv, author, "hello")

	feed := fetchPublicFeed(t, srv, nil)
	// httpd rewrites feed.Uuid == "Public" to the current user; the literal
	// wire values are a contract.
	require.Equal(t, "Public", feed.Uuid)
	require.Equal(t, "Public", feed.Id)
	require.Equal(t, "Everyone's feed", feed.Name)
	require.Equal(t, "group", feed.Type)
	require.False(t, feed.Private)
}

func TestPublicFeedBumpsOnlyNewEntries(t *testing.T) {
	srv := newPublicTestServer(t)
	author := addPublicTestProfile(t, srv, "author", false)

	first := postPublicTestEntry(t, srv, author, "first")
	time.Sleep(2 * time.Millisecond)
	second := postPublicTestEntry(t, srv, author, "second")

	feed := fetchPublicFeed(t, srv, nil)
	require.Equal(t, []string{second.Id, first.Id}, publicFeedEntryIDs(feed))

	// Re-archiving an existing entry must not move it to the front.
	time.Sleep(2 * time.Millisecond)
	_, err := srv.PostEntry(context.Background(), &pb.Entry{
		Id:          first.Id,
		Date:        first.Date,
		Body:        "first re-archived",
		ProfileUuid: author.Uuid,
	})
	require.NoError(t, err)
	feed = fetchPublicFeed(t, srv, nil)
	require.Equal(t, []string{second.Id, first.Id}, publicFeedEntryIDs(feed))
}

func TestPublicFeedExcludesPrivateAndUnknownFeeds(t *testing.T) {
	srv := newPublicTestServer(t)
	private := addPublicTestProfile(t, srv, "private", true)
	postPublicTestEntry(t, srv, private, "secret")

	// An entry whose target feed cannot be resolved must not leak either.
	orphan := &pb.Entry{
		Id:       uuid.Must(uuid.NewV4()).String(),
		FeedUuid: uuid.Must(uuid.NewV4()).String(),
	}
	require.NoError(t, srv.bumpPublicTimeline(orphan, nil))

	feed := fetchPublicFeed(t, srv, nil)
	require.Empty(t, feed.Entries)
}

func TestPublicFeedLikeAndCommentBumpRules(t *testing.T) {
	srv := newPublicTestServer(t)
	ctx := context.Background()
	author := addPublicTestProfile(t, srv, "author", false)
	liker := addPublicTestProfile(t, srv, "liker", false)
	entry := postPublicTestEntry(t, srv, author, "entry")

	published := publicTimelineActivity(t, srv, entry.Id)

	// First Like bumps; a duplicate Like must not.
	time.Sleep(2 * time.Millisecond)
	_, err := srv.LikeEntry(ctx, &pb.LikeRequest{Entry: entry.Id, User: liker.Uuid, Like: true})
	require.NoError(t, err)
	liked := publicTimelineActivity(t, srv, entry.Id)
	require.True(t, liked.After(published))

	time.Sleep(2 * time.Millisecond)
	_, err = srv.LikeEntry(ctx, &pb.LikeRequest{Entry: entry.Id, User: liker.Uuid, Like: true})
	require.NoError(t, err)
	require.Equal(t, liked, publicTimelineActivity(t, srv, entry.Id))

	// Unlike does not move the row either.
	time.Sleep(2 * time.Millisecond)
	_, err = srv.LikeEntry(ctx, &pb.LikeRequest{Entry: entry.Id, User: liker.Uuid, Like: false})
	require.NoError(t, err)
	require.Equal(t, liked, publicTimelineActivity(t, srv, entry.Id))

	// A new Comment bumps; editing it must not.
	comment := &pb.Comment{
		Id:   uuid.Must(uuid.NewV4()).String(),
		Body: "new comment",
	}
	time.Sleep(2 * time.Millisecond)
	_, err = srv.CommentEntry(ctx, &pb.CommentRequest{Entry: entry.Id, Comment: comment, UserUuid: author.Uuid})
	require.NoError(t, err)
	commented := publicTimelineActivity(t, srv, entry.Id)
	require.True(t, commented.After(liked))

	time.Sleep(2 * time.Millisecond)
	comment.Body = "edited comment"
	_, err = srv.CommentEntry(ctx, &pb.CommentRequest{Entry: entry.Id, Comment: comment, UserUuid: author.Uuid})
	require.NoError(t, err)
	require.Equal(t, commented, publicTimelineActivity(t, srv, entry.Id))
}

func TestPublicFeedPagingByStartAndCursor(t *testing.T) {
	srv := newPublicTestServer(t)
	author := addPublicTestProfile(t, srv, "author", false)

	first := postPublicTestEntry(t, srv, author, "first")
	time.Sleep(2 * time.Millisecond)
	second := postPublicTestEntry(t, srv, author, "second")
	time.Sleep(2 * time.Millisecond)
	third := postPublicTestEntry(t, srv, author, "third")

	// Legacy ?start=N links keep working on the new index.
	feed := fetchPublicFeed(t, srv, &pb.FeedRequest{Id: "public", Start: 1, PageSize: 1})
	require.Equal(t, []string{second.Id}, publicFeedEntryIDs(feed))

	// Cursor paging walks the same order without overlap.
	feed = fetchPublicFeed(t, srv, &pb.FeedRequest{Id: "public", PageSize: 2, CursorPaging: true})
	require.Equal(t, []string{third.Id, second.Id}, publicFeedEntryIDs(feed))
	require.NotEmpty(t, feed.NextCursor)

	feed = fetchPublicFeed(t, srv, &pb.FeedRequest{Id: "public", PageSize: 2, CursorPaging: true, Cursor: feed.NextCursor})
	require.Equal(t, []string{first.Id}, publicFeedEntryIDs(feed))
	require.Empty(t, feed.NextCursor)
}

func TestPublicTimelineTrimScheduling(t *testing.T) {
	srv := newPublicTestServer(t)
	entry := uuid.Must(uuid.NewV4())
	require.NoError(t, model.BumpPublicTimeline(srv.rdb, entry, time.Now().UTC()))

	// Arm the scheduler as if the bump budget had been exhausted.
	srv.publicTimelineBumps.Store(publicTimelineTrimEvery)
	srv.schedulePublicTimelineTrim()
	require.Eventually(t, func() bool {
		return !srv.publicTimelineTrimming.Load() && srv.publicTimelineBumps.Load() == 0
	}, 5*time.Second, time.Millisecond)

	// A second schedule while nothing new arrived is a no-op that still
	// releases the trimming guard.
	srv.schedulePublicTimelineTrim()
	require.Eventually(t, func() bool {
		return !srv.publicTimelineTrimming.Load()
	}, 5*time.Second, time.Millisecond)
}

func TestPublicFeedHidesExistingEntryAfterFeedBecomesPrivate(t *testing.T) {
	srv := newPublicTestServer(t)
	author := addPublicTestProfile(t, srv, "author", false)
	entry := postPublicTestEntry(t, srv, author, "published while public")

	require.Contains(t, publicFeedEntryIDs(fetchPublicFeed(t, srv, nil)), entry.Id)

	// Flip the author to private after the entry already entered the public
	// timeline: the stale row must stop rendering on read.
	profile, err := model.GetProfileFromUuid(srv.mdb, uuid.Must(uuid.FromString(author.Uuid)))
	require.NoError(t, err)
	profile.Private = true
	_, err = model.Profile.Put(srv.mdb, uuid.Must(uuid.FromString(author.Uuid)).Bytes(), profile)
	require.NoError(t, err)

	require.NotContains(t, publicFeedEntryIDs(fetchPublicFeed(t, srv, nil)), entry.Id)
}
