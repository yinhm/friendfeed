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

func TestMigrateGroupEntryAuthorsRepairsEntryAndAuthorIndex(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	author := putMigrationProfile(t, db, "author", "user")
	group := putMigrationProfile(t, db, "group", "group")
	published := time.Date(2010, 2, 3, 4, 5, 6, 0, time.UTC)
	entryID := uuid.Must(uuid.NewV4())
	entry := &pb.Entry{
		Id: entryID.String(), Date: published.Format(time.RFC3339),
		ProfileUuid: group.Uuid, FeedUuid: group.Uuid,
		From: &pb.Feed{Id: author.Id, Uuid: group.Uuid, Name: "Old author snapshot", Type: "user"},
		To:   []*pb.Feed{{Id: group.Id, Uuid: group.Uuid, Type: "group"}},
	}
	_, err = model.Entry.Put(db, entryID.Bytes(), entry)
	require.NoError(t, err)
	entryKey := model.Entry.PrefixAppend(entryID.Bytes())
	require.NoError(t, model.EntryIndex.Index(db, uuid.Must(uuid.FromString(group.Uuid)), published, entryKey))

	dry, err := migrateGroupEntryAuthors(db, groupEntryAuthorMigrationOptions{dryRun: true})
	require.NoError(t, err)
	require.Equal(t, 1, dry.fixed)
	stored, err := model.GetEntry(db, entry.Id)
	require.NoError(t, err)
	require.Equal(t, group.Uuid, stored.ProfileUuid)

	stats, err := migrateGroupEntryAuthors(db, groupEntryAuthorMigrationOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, stats.fixed)
	stored, err = model.GetEntry(db, entry.Id)
	require.NoError(t, err)
	require.Equal(t, author.Uuid, stored.ProfileUuid)
	require.Equal(t, author.Uuid, stored.From.Uuid)
	require.Equal(t, group.Uuid, stored.FeedUuid)
	require.Equal(t, group.Uuid, stored.To[0].Uuid)

	authorIndex, err := model.EntryIndexKey(uuid.Must(uuid.FromString(author.Uuid)), entryID, published)
	require.NoError(t, err)
	exists, err := db.Exists(authorIndex)
	require.NoError(t, err)
	require.True(t, exists)
	groupIndex, err := model.EntryIndexKey(uuid.Must(uuid.FromString(group.Uuid)), entryID, published)
	require.NoError(t, err)
	exists, err = db.Exists(groupIndex)
	require.NoError(t, err)
	require.True(t, exists)

	again, err := migrateGroupEntryAuthors(db, groupEntryAuthorMigrationOptions{})
	require.NoError(t, err)
	require.Zero(t, again.fixed)
}

func TestMigrateGroupEntryAuthorsSkipsCanonicalAndUnresolvedEntries(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	group := putMigrationProfile(t, db, "group", "group")
	for _, from := range []*pb.Feed{
		{Id: group.Id, Uuid: group.Uuid, Type: "group"},
		{Id: "missing-user", Uuid: group.Uuid, Type: "user"},
	} {
		entryID := uuid.Must(uuid.NewV4())
		_, err = model.Entry.Put(db, entryID.Bytes(), &pb.Entry{
			Id: entryID.String(), Date: time.Now().UTC().Format(time.RFC3339),
			ProfileUuid: group.Uuid, FeedUuid: group.Uuid, From: from,
		})
		require.NoError(t, err)
	}

	stats, err := migrateGroupEntryAuthors(db, groupEntryAuthorMigrationOptions{})
	require.NoError(t, err)
	require.Zero(t, stats.fixed)
	require.Equal(t, 1, stats.unresolved)
	require.Equal(t, 1, stats.skipped)
}

func putMigrationProfile(t *testing.T, db *store.Store, id, typ string) *pb.Profile {
	t.Helper()
	profile := &pb.Profile{Uuid: uuid.Must(uuid.NewV4()).String(), Id: id, Name: id, Type: typ}
	require.NoError(t, model.UpdateProfile(db, profile))
	return profile
}
