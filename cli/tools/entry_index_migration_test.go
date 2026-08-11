package main

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/store"
)

func TestMigrateEntryIndexDryRunAndApply(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	owner := uuid.Must(uuid.NewV4())
	entryKey := model.Entry.PrefixAppend(uuid.Must(uuid.NewV4()).Bytes())
	legacyKey := store.NewUUIDFlakeKey(model.TableEntryIndex, owner, db.NextId()).Bytes()
	require.NoError(t, db.Put(legacyKey, entryKey))

	stats, err := migrateEntryIndex(db, true, 0)
	require.NoError(t, err)
	require.Equal(t, entryIndexMigrationStats{scanned: 1, migrated: 1}, stats)
	exists, err := db.Exists(legacyKey)
	require.NoError(t, err)
	require.True(t, exists)

	stats, err = migrateEntryIndex(db, false, 0)
	require.NoError(t, err)
	require.Equal(t, entryIndexMigrationStats{scanned: 1, migrated: 1}, stats)
	exists, err = db.Exists(legacyKey)
	require.NoError(t, err)
	require.False(t, exists)
	currentKey := append(append([]byte(nil), legacyKey...), entryKey...)
	exists, err = db.Exists(currentKey)
	require.NoError(t, err)
	require.True(t, exists)

	stats, err = migrateEntryIndex(db, false, 0)
	require.NoError(t, err)
	require.Equal(t, entryIndexMigrationStats{scanned: 1, current: 1}, stats)
}
