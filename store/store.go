package store

import (
	"errors"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
	"github.com/golang/glog"
	"github.com/yinhm/friendfeed/store/flake"
)

type ScanCallback func(int, []byte, []byte) error

type Error struct {
	Msg  string
	Code ErrorCode
}

func (e *Error) Error() string { return e.Msg }

type Store struct {
	dbpath  string
	rdb     *pebble.DB
	options *pebble.Options
	// ro      *pebble.ReadOptions
	wo *pebble.WriteOptions

	closed bool
	idGen  *flake.Generator

	syncWrites atomic.Bool
}

func NewStore(dbpath string) *Store {
	if err := mkdir(dbpath); err != nil {
		glog.Fatalf("Can not create db: %s", err)
	}

	db := new(Store)
	db.dbpath = dbpath
	db.options = NewStoreOptions()
	db.initWriteOptions()

	// TODO: shared cache for meta store
	cacheSize := 512 << 20 // 512 MB
	pebbleCache := pebble.NewCache(int64(cacheSize))
	defer pebbleCache.Unref()

	db.options.Cache = pebbleCache

	rdb, err := pebble.Open(dbpath, db.options)
	if err != nil {
		log.Fatalf("Can not open db: %s", err)
	}
	db.rdb = rdb
	db.idGen = flake.NewGenerator()
	db.closed = false

	db.syncWrites.Store(true)

	return db
}

func NewMetaStore(dbpath string) *Store {
	db := new(Store)
	db.dbpath = dbpath
	db.options = NewMetaStoreOptions()
	db.initWriteOptions()

	// TODO: shared cache
	cacheSize := 128 << 20 // 512 MB
	pebbleCache := pebble.NewCache(int64(cacheSize))
	defer pebbleCache.Unref()

	db.options.Cache = pebbleCache

	rdb, err := pebble.Open(dbpath, db.options)
	if err != nil {
		log.Fatalf("Can not open db: %s", err)
	}
	db.rdb = rdb
	db.idGen = flake.NewGenerator()
	db.syncWrites.Store(true)

	return db
}

func DestroyStore(dbpath string, options *pebble.Options) error {
	return errors.New("DestroyStore not implemented...")
}

func NewStoreOptions() *pebble.Options {
	opts := &pebble.Options{
		// suggest in: https://github.com/cockroachdb/pebble/issues/1068#issuecomment-784208214
		L0CompactionThreshold: 2,
		L0StopWritesThreshold: 1000,
		MaxConcurrentCompactions: func() int {
			return 3
		},
		LBaseMaxBytes:               64 << 20, // 64 MB
		Levels:                      make([]pebble.LevelOptions, 7),
		MemTableSize:                64 << 20, // 64 MB
		MemTableStopWritesThreshold: 4,

		// EventListener: pebble.MakeLoggingEventListener(pebble.DefaultLogger),
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

func NewMetaStoreOptions() *pebble.Options {
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

func (db *Store) initWriteOptions() {
	db.wo = &pebble.WriteOptions{Sync: false}
}

func (db *Store) Close() {
	if db.closed {
		log.Print("closing unopened pebble instance")
		return
	}
	db.rdb.Close()
	db.closed = true
}

func (db *Store) Destroy() error {
	log.Printf("WARN: destroy path %s", db.dbpath)
	return os.RemoveAll(db.dbpath)
}

func (db *Store) Options() *pebble.Options {
	return db.options
}

func (db *Store) SetSync(syncOrNot bool) {
	db.syncWrites.Store(syncOrNot)
}

func (db *Store) writeOptions() *pebble.WriteOptions {
	if db.syncWrites.Load() {
		return pebble.Sync
	}
	return pebble.NoSync
}

func (db *Store) Flush() {
	db.rdb.Flush()
}

func (db *Store) Get(key []byte) ([]byte, error) {
	value, closer, err := db.rdb.Get(key)
	if closer != nil {
		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)
		value = valueCopy
		closer.Close()
	}
	if errors.Is(err, pebble.ErrNotFound) || len(value) == 0 {
		return nil, nil
	}
	return value, err
}

func (db *Store) Put(key, value []byte) error {
	if len(key) == 0 {
		return errors.New("empty key")
	}
	return db.rdb.Set(key, value, db.writeOptions())
}

func (db *Store) Set(key, value []byte) error {
	return db.Put(key, value)
}

func (db *Store) Delete(key []byte) error {
	if len(key) == 0 {
		return errors.New("empty key")
	}
	return db.rdb.Delete(key, db.writeOptions())
}

func (db *Store) Exist(key []byte) bool {
	data, err := db.Get(key)
	if err != nil || len(data) == 0 {
		return false
	}
	return true
}

func (db *Store) NewBatch() *pebble.Batch {
	return db.rdb.NewBatch()
}

func (db *Store) Metrics() *pebble.Metrics {
	return db.rdb.Metrics()
}

func (db *Store) Iterator() *Iterator {
	opts := &pebble.IterOptions{}
	return newIterator(db.rdb, opts)
}

func (db *Store) NewIterator(prefix Key) *Iterator {
	opts := PrefixIteratorOptions(prefix)
	return newIterator(db.rdb, opts)
}

func (db *Store) NextId() flake.Id {
	for {
		fid, err := db.idGen.NextId()
		if err == nil {
			return fid
		}
		log.Printf("Error on NextId: %s", err)
		time.Sleep(1 * time.Second)
	}
}

func (db *Store) TimeTravelId(t time.Time) flake.Id {
	gen := flake.NewGeneratorFromTime(t)
	fid, _ := gen.NextId()
	return fid
}

// BUG: see flake.NewWorkerId
// Use const WorkId for now till the index design changes.
func (db *Store) TimeTravelReverseId(t time.Time) flake.Id {
	duration := flake.MaxTime.Sub(t)
	reverseTime := time.Unix(int64(duration.Seconds()), 0)
	gen := flake.NewGeneratorFromTime(reverseTime)
	fid, _ := gen.NextId()
	return fid
}

func (db *Store) ForwardScan(prefix Key, fn ScanCallback) (n int, err error) {
	opts := PrefixIteratorOptions(prefix)
	iter := newIterator(db.rdb, opts)
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()
		if err = fn(n, key, value); err != nil {
			var serr *Error
			if errors.As(err, &serr) {
				if serr.Code == StopIteration { // do not remove
					return n, nil // rewrote err
				}
			}
			return
		}
		n++
	}
	return n, nil
}
