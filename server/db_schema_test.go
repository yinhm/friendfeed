package server

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/store"
)

func TestPrepareRuntimeSchemaInitializesEmptyDatabase(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, prepareRuntimeSchema(db))
	info, err := model.InspectDBSchema(db)
	require.NoError(t, err)
	require.Equal(t, model.DBSchemaCurrent, info.Status)
}

func TestPrepareRuntimeSchemaAllowsUnversionedExistingDatabaseInV22(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.Set(model.TableMeta.Bytes(), []byte("existing")))
	require.NoError(t, prepareRuntimeSchema(db))
	info, err := model.InspectDBSchema(db)
	require.NoError(t, err)
	require.Equal(t, model.DBSchemaMissing, info.Status)
}

func TestPrepareRuntimeSchemaRejectsFutureAndMalformedMarkers(t *testing.T) {
	for _, raw := range [][]byte{{1}, schemaVersionRaw(model.CurrentDBSchemaVersion + 1)} {
		db, err := store.NewStore(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, db.Set(model.DBSchemaVersionKey(), raw))
		require.Error(t, prepareRuntimeSchema(db))
		db.Close()
	}
}

func schemaVersionRaw(version uint32) []byte {
	raw := make([]byte, 4)
	binary.BigEndian.PutUint32(raw, version)
	return raw
}
