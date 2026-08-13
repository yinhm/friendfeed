package main

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/store/flake"
)

func TestMigrateEntryIndexDryRunAndApply(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	owner := uuid.Must(uuid.NewV4())
	entryID := uuid.Must(uuid.NewV4())
	entryKey := model.Entry.PrefixAppend(entryID.Bytes())
	flakeID := db.NextId()
	legacyKey := store.NewUUIDFlakeKey(model.TableEntryIndex, owner, flakeID).Bytes()
	require.NoError(t, db.Put(legacyKey, entryKey))
	previousKey := model.NewKeyFrom(legacyKey, entryKey)
	require.NoError(t, db.Put(previousKey, entryKey))

	// The legacy reverse Flake stores MaxTime - entry time in whole seconds.
	forward := time.UnixMilli(flake.MaxTime.UnixMilli() - int64(binary.BigEndian.Uint64(flakeID[0:8]))).UTC()
	currentKey, err := model.EntryIndexKey(owner, entryID, forward)
	require.NoError(t, err)

	stats, err := migrateEntryIndex(db, true, 0)
	require.NoError(t, err)
	require.Equal(t, entryIndexMigrationStats{scanned: 2, migrated: 2}, stats)
	exists, err := db.Exists(legacyKey)
	require.NoError(t, err)
	require.True(t, exists)

	stats, err = migrateEntryIndex(db, false, 0)
	require.NoError(t, err)
	require.Equal(t, entryIndexMigrationStats{scanned: 2, migrated: 2}, stats)
	for _, old := range [][]byte{legacyKey, previousKey} {
		exists, err := db.Exists(old)
		require.NoError(t, err)
		require.False(t, exists)
	}
	// Both legacy layouts collapse onto the single current key, value empty.
	value, err := db.Get(currentKey)
	require.NoError(t, err)
	require.Empty(t, value)

	stats, err = migrateEntryIndex(db, false, 0)
	require.NoError(t, err)
	require.Equal(t, entryIndexMigrationStats{scanned: 1, current: 1}, stats)
}
