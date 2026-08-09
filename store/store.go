package store

import (
	"errors"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
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

	closeOnce sync.Once
	closeErr  error
	idGen     *flake.Generator

	syncWrites atomic.Bool
	batchMu    sync.Mutex
}

func NewStore(dbpath string) *Store {
	db, err := NewStoreWithError(dbpath)
	if err != nil {
		log.Fatalf("Can not create or open db: %s", err)
	}
	return db
}

// NewStoreWithError creates or opens a Store without terminating the process.
// Online request paths must use this variant so operational failures can be
// returned to the caller and normal server shutdown remains possible.
func NewStoreWithError(dbpath string) (*Store, error) {
	if err := mkdir(dbpath); err != nil {
		return nil, err
	}
	return openStoreWithError(dbpath, NewStoreOptions(), 512<<20)
}

// NewStoreReadOnly opens an existing database without creating or mutating
// on-disk state; writes through the returned Store fail. Pebble still requires
// the database lock, so this cannot inspect a directory held open by another
// process. Stop that process or inspect a consistent backup instead.
func NewStoreReadOnly(dbpath string) *Store {
	opts := NewStoreOptions()
	opts.ReadOnly = true
	return openStore(dbpath, opts, 512<<20)
}

func openStore(dbpath string, options *pebble.Options, cacheSize int64) *Store {
	db, err := openStoreWithError(dbpath, options, cacheSize)
	if err != nil {
		log.Fatalf("Can not open db: %s", err)
	}
	return db
}

func openStoreWithError(dbpath string, options *pebble.Options, cacheSize int64) (*Store, error) {
	db := &Store{
		dbpath:  dbpath,
		options: options,
		idGen:   flake.NewGenerator(),
	}
	db.initWriteOptions()

	// TODO: shared cache for main and meta stores
	pebbleCache := pebble.NewCache(cacheSize)
	defer pebbleCache.Unref()
	db.options.Cache = pebbleCache

	rdb, err := pebble.Open(dbpath, db.options)
	if err != nil {
		return nil, err
	}
	db.rdb = rdb
	db.syncWrites.Store(true)

	return db, nil
}

func DestroyStore(dbpath string, options *pebble.Options) error {
	return errors.New("DestroyStore not implemented...")
}

func NewStoreOptions() *pebble.Options {
	opts := &pebble.Options{
		// suggest in: https://github.com/cockroachdb/pebble/issues/1068#issuecomment-784208214
		L0CompactionThreshold: 2,
		L0StopWritesThreshold: 1000,
		CompactionConcurrencyRange: func() (int, int) {
			// v1 MaxConcurrentCompactions was a hard cap of 3 with dynamic
			// scaling from 1; (1, 3) preserves that behavior.
			return 1, 3
		},
		LBaseMaxBytes:               64 << 20, // 64 MB
		MemTableSize:                64 << 20, // 64 MB
		MemTableStopWritesThreshold: 4,

		// EventListener: pebble.MakeLoggingEventListener(pebble.DefaultLogger),
	}

	configureLevels(opts)
	return opts
}

func configureLevels(opts *pebble.Options) {
	// TargetFileSizes is deliberately left unset: Options.EnsureDefaults
	// (invoked by Open) defaults it to 2 MB at L0, doubling per level,
	// which matches the pebble v1 configuration.
	for i := 0; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10       // 32 KB
		l.IndexBlockSize = 256 << 10 // 256 KB
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		if i > 0 {
			l.EnsureL1PlusDefaults(&opts.Levels[i-1])
		} else {
			l.EnsureL0Defaults()
		}
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
}

func (db *Store) initWriteOptions() {
	db.wo = &pebble.WriteOptions{Sync: false}
}

func (db *Store) Close() {
	if err := db.CloseWithError(); err != nil {
		log.Printf("close pebble store: %v", err)
	}
}

// CloseWithError closes the store and returns Pebble's close error. Close is
// retained for compatibility; callers that must not publish output unless all
// writes are durable, such as backups, should use this variant.
func (db *Store) CloseWithError() error {
	db.closeOnce.Do(func() {
		db.closeErr = db.rdb.Close()
	})
	return db.closeErr
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

// ApplyBatch builds and commits one atomic Pebble batch while serializing
// other ApplyBatch callers on this Store. The callback may perform reads
// through db before adding writes to the batch; returning an error aborts
// the batch without committing it. Commit honors SetSync.
func (db *Store) ApplyBatch(fn func(*pebble.Batch) error) error {
	db.batchMu.Lock()
	defer db.batchMu.Unlock()

	batch := db.rdb.NewBatch()
	defer batch.Close()
	if err := fn(batch); err != nil {
		return err
	}
	if batch.Count() == 0 {
		return nil
	}
	return batch.Commit(db.writeOptions())
}

func (db *Store) Metrics() *pebble.Metrics {
	return db.rdb.Metrics()
}

func (db *Store) Iterator() *Iterator {
	opts := &pebble.IterOptions{}
	return newIterator(db.rdb, opts)
}

// Snapshot returns a point-in-time, consistent view of the database: reads
// through it observe the state as of its creation, unaffected by later
// writes or deletes. The caller must Close the snapshot after use.
func (db *Store) Snapshot() *pebble.Snapshot {
	return db.rdb.NewSnapshot()
}

// SnapshotIterator returns a full-range iterator reading from snap. The
// caller must Close the iterator, and Close the snapshot after the iterator.
func (db *Store) SnapshotIterator(snap *pebble.Snapshot) *Iterator {
	return newIterator(snap, &pebble.IterOptions{})
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
