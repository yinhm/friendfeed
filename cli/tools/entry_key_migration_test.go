package main

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func putLegacyStringKeyEntry(t *testing.T, db *store.Store, entry *pb.Entry) store.Key {
	t.Helper()
	raw, err := proto.Marshal(entry)
	require.NoError(t, err)
	key := model.NewKeyFrom(model.Entry.Prefix, []byte(entry.Id))
	require.NoError(t, db.Put(key, raw))
	return key
}

func TestMigrateEntryKeysDryRunApplyAndIdempotence(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	entryID := uuid.Must(uuid.NewV4())
	entry := &pb.Entry{Id: entryID.String(), Body: "legacy"}
	legacyKey := putLegacyStringKeyEntry(t, db, entry)
	canonical := model.Entry.PrefixAppend(entryID.Bytes())

	dry, err := migrateEntryKeys(db, true)
	require.NoError(t, err)
	require.Equal(t, entryKeyMigrationStats{scanned: 1, migrated: 1}, dry)
	_, err = db.Get(legacyKey)
	require.NoError(t, err)
	_, err = db.Get(canonical)
	require.ErrorIs(t, err, store.ErrNotFound)

	stats, err := migrateEntryKeys(db, false)
	require.NoError(t, err)
	require.Equal(t, dry, stats)
	_, err = db.Get(legacyKey)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = db.Get(canonical)
	require.NoError(t, err)

	again, err := migrateEntryKeys(db, false)
	require.NoError(t, err)
	require.Equal(t, entryKeyMigrationStats{scanned: 1, canonical: 1}, again)
}

func TestMigrateEntryKeysRejectsConflictWithoutPartialWrites(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	firstID := uuid.Must(uuid.NewV4())
	first := &pb.Entry{Id: firstID.String(), Body: "first"}
	firstLegacy := putLegacyStringKeyEntry(t, db, first)
	conflictID := uuid.Must(uuid.NewV4())
	conflictLegacy := putLegacyStringKeyEntry(t, db, &pb.Entry{Id: conflictID.String(), Body: "old"})
	_, err = model.Entry.Put(db, conflictID.Bytes(), &pb.Entry{Id: conflictID.String(), Body: "different"})
	require.NoError(t, err)

	_, err = migrateEntryKeys(db, false)
	require.ErrorContains(t, err, "conflicts with canonical")
	for _, key := range []store.Key{firstLegacy, conflictLegacy} {
		_, getErr := db.Get(key)
		require.NoError(t, getErr, "preflight failure must not migrate earlier rows")
	}
	_, err = db.Get(model.Entry.PrefixAppend(firstID.Bytes()))
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestMigrateEntryKeysRejectsKeyAndValueUUIDMismatch(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	entryID := uuid.Must(uuid.NewV4())
	raw, err := proto.Marshal(&pb.Entry{Id: entryID.String()})
	require.NoError(t, err)
	wrong := uuid.Must(uuid.NewV4())
	require.NoError(t, db.Put(model.NewKeyFrom(model.Entry.Prefix, []byte(wrong.String())), raw))

	_, err = migrateEntryKeys(db, false)
	require.ErrorContains(t, err, "disagrees with Id")
}
