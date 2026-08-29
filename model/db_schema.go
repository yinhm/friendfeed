package model

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/yinhm/friendfeed/store"
)

const CurrentDBSchemaVersion uint32 = 1

var dbSchemaVersionKey = NewKeyFrom(TableMeta.Bytes(), []byte("db-schema/version"))

type DBSchemaStatus string

const (
	DBSchemaMissing   DBSchemaStatus = "missing"
	DBSchemaCurrent   DBSchemaStatus = "current"
	DBSchemaOlder     DBSchemaStatus = "older"
	DBSchemaFuture    DBSchemaStatus = "future"
	DBSchemaMalformed DBSchemaStatus = "malformed"
)

type DBSchemaInfo struct {
	Status  DBSchemaStatus
	Version uint32
}

// DBSchemaVersionKey returns a copy of the application schema marker key.
func DBSchemaVersionKey() store.Key {
	return append(store.Key(nil), dbSchemaVersionKey...)
}

func InspectDBSchema(db *store.Store) (DBSchemaInfo, error) {
	raw, err := db.Get(dbSchemaVersionKey)
	if errors.Is(err, store.ErrNotFound) {
		return DBSchemaInfo{Status: DBSchemaMissing}, nil
	}
	if err != nil {
		return DBSchemaInfo{}, fmt.Errorf("read database schema marker: %w", err)
	}
	if len(raw) != 4 {
		return DBSchemaInfo{Status: DBSchemaMalformed}, nil
	}
	version := binary.BigEndian.Uint32(raw)
	return DBSchemaInfo{Status: classifyDBSchemaVersion(version, CurrentDBSchemaVersion), Version: version}, nil
}

func classifyDBSchemaVersion(version, current uint32) DBSchemaStatus {
	status := DBSchemaCurrent
	switch {
	case version == 0:
		status = DBSchemaMalformed
	case version < current:
		status = DBSchemaOlder
	case version > current:
		status = DBSchemaFuture
	}
	return status
}

// StoreHasRecords reports whether the application keyspace contains any row.
// Pebble metadata and files are intentionally irrelevant to this decision.
func StoreHasRecords(db *store.Store) (bool, error) {
	iter, err := db.Iterator()
	if err != nil {
		return false, err
	}
	defer iter.Close()
	return iter.First(), iter.Error()
}

func PutDBSchemaVersion(db *store.Store, version uint32) error {
	if version != CurrentDBSchemaVersion {
		return fmt.Errorf("database schema version must be current version %d", CurrentDBSchemaVersion)
	}
	info, err := InspectDBSchema(db)
	if err != nil {
		return err
	}
	switch info.Status {
	case DBSchemaCurrent:
		return nil
	case DBSchemaFuture, DBSchemaMalformed:
		return fmt.Errorf("refuse to replace %s database schema marker", info.Status)
	}
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], version)
	return db.ApplyBatch(func(batch *pebble.Batch) error {
		return batch.Set(dbSchemaVersionKey, raw[:], nil)
	})
}
