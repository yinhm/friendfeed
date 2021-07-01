package store

import (
	"log"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
)

type KeyPrefix uint32

const (
	TableConfig KeyPrefix = 100
	TableStock  KeyPrefix = 101
)

type Store struct {
	bdb *pebble.DB

	path   string
	closed bool
}

// DefaultPebbleOptions returns the default pebble options.
func DefaultPebbleOptions() *pebble.Options {
	opts := &pebble.Options{
		LBaseMaxBytes:               64 << 20, // 64 MB
		Levels:                      make([]pebble.LevelOptions, 7),
		MemTableSize:                64 << 20, // 64 MB
		MemTableStopWritesThreshold: 4,
	}
	for i := 0; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10       // 32 KB
		l.IndexBlockSize = 256 << 10 // 256 KB
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		if i > 0 {
			l.TargetFileSize = opts.Levels[i-1].TargetFileSize * 2
		}
		l.EnsureDefaults()
	}

	// Do not create bloom filters for the last level (i.e. the largest level
	// which contains data in the LSM store). This configuration reduces the size
	// of the bloom filters by 10x. This is significant given that bloom filters
	// require 1.25 bytes (10 bits) per key which can translate into gigabytes of
	// memory given typical key and value sizes. The downside is that bloom
	// filters will only be usable on the higher levels, but that seems
	// acceptable. We typically see read amplification of 5-6x on clusters
	// (i.e. there are 5-6 levels of sstables) which means we'll achieve 80-90%
	// of the benefit of having bloom filters on every level for only 10% of the
	// memory cost.
	opts.Levels[6].FilterPolicy = nil

	return opts
}

func NewStore(dbpath string) *Store {
	if err := mkdir(dbpath); err != nil {
		log.Fatalf("Can not create db: %s", err)
	}

	opts := DefaultPebbleOptions()
	db, err := pebble.Open(dbpath, opts)
	if err != nil {
		log.Fatalf("Can not open db: %s", err)
	}

	return &Store{
		path: dbpath,
		bdb:  db,
	}
}

func InitTables(db *Store) {
	if TableInited {
		log.Fatalf("table inited")
	}
	TConfig.InitStore(db)
	TStock.InitStore(db)
	TableInited = true
}

func (db *Store) String() string {
	dir := db.path
	if dir == "" {
		dir = "<in-mem>"
	}
	return dir
}

func (db *Store) Close() {
	if db.closed {
		log.Print("closing unopened pebble instance")
		return
	}
	db.bdb.Close()
	db.closed = true
}

func (db *Store) Get(key []byte) (v []byte, err error) {
	v, closer, err := db.bdb.Get(key)
	if err != nil {
		return v, err
	}
	if err = closer.Close(); err != nil {
		return v, err
	}
	return v, err
}

func (db *Store) rawGet(key []byte) ([]byte, error) {
	v, closer, err := db.bdb.Get(key)
	if closer != nil {
		vv := make([]byte, len(v))
		copy(vv, v)
		v = vv
		closer.Close()
	}
	if err == pebble.ErrNotFound || len(v) == 0 {
		return nil, nil
	}
	return v, err
}

func (db *Store) Set(key, value []byte) error {
	return db.bdb.Set(key, value, pebble.NoSync)
}

func (db *Store) Delete(key []byte) error {
	return db.bdb.Delete(key, pebble.NoSync)
}
