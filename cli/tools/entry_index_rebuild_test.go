package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestRebuildEntryIndexesRestoresSourceDerivedRows(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	author := uuid.Must(uuid.NewV4())
	follower := uuid.Must(uuid.NewV4())
	require.NoError(t, db.Put(model.NewKeyFrom(model.Follow.Prefix, follower.Bytes(), author.Bytes()), []byte("1")))
	require.NoError(t, db.Put(model.NewKeyFrom(model.Follower.Prefix, author.Bytes(), follower.Bytes()), []byte("1")))
	date := time.Now().UTC().Truncate(time.Second)
	entryKeys := make([]store.Key, 0, 2)
	for range 2 {
		entryID := uuid.Must(uuid.NewV4())
		key, err := model.PutEntry(db, &pb.Entry{
			Id: entryID.String(), Date: date.Format(time.RFC3339), ProfileUuid: author.String(),
			From: &pb.Feed{Uuid: author.String(), Id: "author"},
		})
		require.NoError(t, err)
		entryKeys = append(entryKeys, key)
	}
	require.NoError(t, model.EntryIndex.RemoveIndex(db, author, date, entryKeys[0]))
	orphan := store.NewUUIDFlakeKey(model.TableEntryIndex, uuid.Must(uuid.NewV4()), db.NextId())
	require.NoError(t, db.Put(orphan.Bytes(), model.Entry.PrefixAppend(uuid.Must(uuid.NewV4()).Bytes())))

	dry, err := rebuildEntryIndexes(db, entryIndexRebuildOptions{dryRun: true})
	require.NoError(t, err)
	require.Equal(t, 2, dry.entries)
	require.Equal(t, 2, dry.direct)
	require.Zero(t, dry.removed)
	require.Equal(t, 1, dry.feedsChecked)
	require.NotZero(t, dry.feedsMismatched)

	stats, err := rebuildEntryIndexes(db, entryIndexRebuildOptions{})
	require.NoError(t, err)
	require.Equal(t, 2, stats.removed)
	audit, err := auditStore(db)
	require.NoError(t, err)
	require.Zero(t, audit.missingDirectIndexes)
	require.Zero(t, audit.orphanIndexes)
	require.Equal(t, 2, audit.entryIndexes)

	verified, err := rebuildEntryIndexes(db, entryIndexRebuildOptions{dryRun: true})
	require.NoError(t, err)
	require.Equal(t, 1, verified.feedsChecked)
	require.Zero(t, verified.feedsMismatched)
	require.Zero(t, verified.duplicateIndexes)
}

func TestRebuildEntryIndexesForOneUserIsBounded(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	selected := seedActorProfile(t, db, "rebuild-selected")
	other := seedActorProfile(t, db, "rebuild-other")
	date := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	selectedEntry, err := model.PutEntry(db, &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), Date: date.Format(time.RFC3339),
		ProfileUuid: selected.String(), From: &pb.Feed{Uuid: selected.String()},
	})
	require.NoError(t, err)
	otherEntry, err := model.PutEntry(db, &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), Date: date.Add(time.Minute).Format(time.RFC3339),
		ProfileUuid: other.String(), From: &pb.Feed{Uuid: other.String()},
	})
	require.NoError(t, err)
	require.NoError(t, model.EntryIndex.RemoveIndex(db, selected, date, selectedEntry))

	stats, err := rebuildEntryIndexes(db, entryIndexRebuildOptions{
		user: "rebuild-selected", maxLimit: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 1, stats.entries)
	require.Equal(t, 1, stats.direct)
	require.Zero(t, stats.removed, "a scoped rebuild must not clear the shared index table")

	assertIndexed := func(owner uuid.UUID, entryKey store.Key) {
		t.Helper()
		found := false
		prefix := store.NewUUIDKey(model.TableEntryIndex, owner).Bytes()
		_, err := db.ForwardScan(prefix, func(_ int, _, value []byte) error {
			found = found || bytes.Equal(value, entryKey)
			return nil
		})
		require.NoError(t, err)
		require.True(t, found)
	}
	assertIndexed(selected, selectedEntry)
	assertIndexed(other, otherEntry)
}

func TestRebuildEntryIndexesRequiresCanonicalEntryKeys(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	profileID := seedActorProfile(t, db, "legacy-entry-key")
	entryID := uuid.Must(uuid.NewV4())
	putLegacyStringKeyEntry(t, db, &pb.Entry{
		Id: entryID.String(), Date: time.Now().UTC().Format(time.RFC3339),
		ProfileUuid: profileID.String(),
	})
	_, err = rebuildEntryIndexes(db, entryIndexRebuildOptions{})
	require.ErrorContains(t, err, "run migrate_entry_keys")
}
