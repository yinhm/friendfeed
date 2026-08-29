package model

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/store"
)

func TestDBSchemaMarkerEncodingAndStates(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	require.Equal(t, append(TableMeta.Bytes(), []byte("db-schema/version")...), []byte(DBSchemaVersionKey()))
	info, err := InspectDBSchema(db)
	require.NoError(t, err)
	require.Equal(t, DBSchemaMissing, info.Status)
	empty, err := StoreHasRecords(db)
	require.NoError(t, err)
	require.False(t, empty)
	require.Equal(t, DBSchemaOlder, classifyDBSchemaVersion(1, 2))

	for _, tt := range []struct {
		name    string
		raw     []byte
		status  DBSchemaStatus
		version uint32
	}{
		{name: "malformed length", raw: []byte{1}, status: DBSchemaMalformed},
		{name: "zero", raw: make([]byte, 4), status: DBSchemaMalformed},
		{name: "current", raw: schemaVersionBytes(CurrentDBSchemaVersion), status: DBSchemaCurrent, version: CurrentDBSchemaVersion},
		{name: "future", raw: schemaVersionBytes(CurrentDBSchemaVersion + 1), status: DBSchemaFuture, version: CurrentDBSchemaVersion + 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, db.Set(DBSchemaVersionKey(), tt.raw))
			info, err := InspectDBSchema(db)
			require.NoError(t, err)
			require.Equal(t, tt.status, info.Status)
			require.Equal(t, tt.version, info.Version)
		})
	}
}

func TestPutDBSchemaVersionPersistsWithoutFlush(t *testing.T) {
	path := t.TempDir()
	db, err := store.NewStore(path)
	require.NoError(t, err)
	require.Error(t, PutDBSchemaVersion(db, 0))
	require.NoError(t, PutDBSchemaVersion(db, CurrentDBSchemaVersion))
	require.NoError(t, db.CloseWithError())

	db, err = store.NewStore(path)
	require.NoError(t, err)
	defer db.Close()
	info, err := InspectDBSchema(db)
	require.NoError(t, err)
	require.Equal(t, DBSchemaInfo{Status: DBSchemaCurrent, Version: CurrentDBSchemaVersion}, info)
	hasRecords, err := StoreHasRecords(db)
	require.NoError(t, err)
	require.True(t, hasRecords)
}

func TestPutDBSchemaVersionDoesNotReplaceInvalidMarkers(t *testing.T) {
	for _, raw := range [][]byte{{1}, schemaVersionBytes(CurrentDBSchemaVersion + 1)} {
		db, err := store.NewStore(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, db.Set(DBSchemaVersionKey(), raw))
		require.Error(t, PutDBSchemaVersion(db, CurrentDBSchemaVersion))
		got, err := db.Get(DBSchemaVersionKey())
		require.NoError(t, err)
		require.Equal(t, raw, got)
		db.Close()
	}
}

func schemaVersionBytes(version uint32) []byte {
	raw := make([]byte, 4)
	binary.BigEndian.PutUint32(raw, version)
	return raw
}
