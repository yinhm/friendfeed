package main

import (
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/search"
	"github.com/yinhm/friendfeed/store"
)

func putSearchableEntry(t *testing.T, db *store.Store, profileID uuid.UUID, ts time.Time, body string) string {
	t.Helper()
	entryID := uuid.Must(uuid.NewV4())
	entry := &pb.Entry{Id: entryID.String(), Body: body, ProfileUuid: profileID.String()}
	key, err := model.Entry.Put(db, entryID.Bytes(), entry)
	require.NoError(t, err)
	require.NoError(t, model.EntryIndex.Index(db, profileID, ts, key))
	return entryID.String()
}

func TestRebuildSearchIndex(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	db.SetSync(false)

	activeID := uuid.Must(uuid.NewV4())
	inactiveID := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{Uuid: activeID.String(), Id: "active", Type: "user"}))
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{Uuid: inactiveID.String(), Id: "inactive", Type: "user"}))
	_, err = model.OAuth.Put(db, model.KeyFromString("twitter", "active-user"), &pb.OAuthUser{
		Uuid: activeID.String(), Provider: "twitter", UserId: "active-user",
	})
	require.NoError(t, err)

	indexedID := putSearchableEntry(t, db, activeID, time.Unix(100, 0), "hello search world")
	putSearchableEntry(t, db, activeID, time.Unix(200, 0), "second historical entry")
	putSearchableEntry(t, db, activeID, time.Unix(300, 0), "") // no body, mirrored from PutEntry
	putSearchableEntry(t, db, inactiveID, time.Unix(400, 0), "entry without oauth login")
	// Index row whose entry record is gone.
	missingKey := model.Entry.PrefixAppend(uuid.Must(uuid.NewV4()).Bytes())
	require.NoError(t, model.EntryIndex.Index(db, activeID, time.Unix(500, 0), missingKey))

	// Dry-run counts without touching the search index.
	dryStats, err := rebuildSearchIndex(db, nil, searchIndexOptions{dryRun: true})
	require.NoError(t, err)
	require.Equal(t, searchIndexStats{profiles: 1, entries: 2, missing: 1, noBody: 1}, dryStats)

	idx, err := search.OpenIndex(t.TempDir())
	require.NoError(t, err)
	defer idx.Close()

	stats, err := rebuildSearchIndex(db, idx, searchIndexOptions{})
	require.NoError(t, err)
	require.Equal(t, dryStats, stats)

	count, err := idx.DocCount()
	require.NoError(t, err)
	require.Equal(t, uint64(2), count)

	query := bleve.NewMatchQuery("hello")
	result, err := idx.Search(bleve.NewSearchRequest(query))
	require.NoError(t, err)
	require.Equal(t, 1, int(result.Total))
	require.Equal(t, indexedID, result.Hits[0].ID)
}

func TestRebuildSearchIndexExplicitUserBypassesOAuth(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	db.SetSync(false)

	userID := uuid.Must(uuid.NewV4())
	require.NoError(t, model.UpdateProfile(db, &pb.Profile{Uuid: userID.String(), Id: "yinhm", Type: "user"}))
	putSearchableEntry(t, db, userID, time.Unix(100, 0), "standalone entry")

	idx, err := search.OpenIndex(t.TempDir())
	require.NoError(t, err)
	defer idx.Close()

	stats, err := rebuildSearchIndex(db, idx, searchIndexOptions{user: "yinhm"})
	require.NoError(t, err)
	require.Equal(t, 1, stats.profiles)
	require.Equal(t, 1, stats.entries)

	count, err := idx.DocCount()
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)

	_, err = rebuildSearchIndex(db, idx, searchIndexOptions{user: "ghost"})
	require.Error(t, err)
}
