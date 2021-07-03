package store

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"
	"unsafe"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
	"github.com/golang/glog"
	uuid "github.com/satori/go.uuid"
	"github.com/yinhm/friendfeed/storage/flake"
)

type PrefixTable uint32

const (
	TableFeed     PrefixTable = 1
	TableFeedinfo PrefixTable = 2
	TableEntry    PrefixTable = 3

	// TODO: obsoleted TableEntryIndex, FixMaxEntryIndex
	// WARN: TableEntryIndex > TableEntry for FixMaxEntryIndex
	TableEntryIndex PrefixTable = 4
	// TableEntryIndex NOT working, BackwardFetchFeed broken
	// duplicate a reverse index
	TableReverseEntryIndex PrefixTable = 5
	TableIndexCache        PrefixTable = 6

	TableProfile      PrefixTable = 100
	TableService      PrefixTable = 101
	TableSubscription PrefixTable = 102
	TableSubscriber   PrefixTable = 103
	TableOAuthTwitter PrefixTable = 104
	TableOAuthGoogle  PrefixTable = 105

	TableJobFeed    PrefixTable = 200
	TableJobRunning PrefixTable = 201
	TableJobHistory PrefixTable = 202

	TableMax PrefixTable = 1e8

	defaultWorkerId     = 1
	defaultDatacenterId = 1
)

type Store struct {
	dbpath  string
	rdb     *pebble.DB
	options *pebble.Options
	// ro      *pebble.ReadOptions
	wo *pebble.WriteOptions

	closed bool
	idGen  *flake.Generator
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

	return db
}

func DestroyStore(dbpath string, options *pebble.Options) error {
	return fmt.Errorf("DestroyStore not implemented...")
}

func NewStoreOptions() *pebble.Options {
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
	// log.Printf("WARN: destroy path %s", db.dbpath)
	return fmt.Errorf("destroy db not implemented...")
}

func (db *Store) Options() *pebble.Options {
	return db.options
}

func (db *Store) Get(key []byte) ([]byte, error) {
	value, closer, err := db.rdb.Get(key)
	if closer != nil {
		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)
		value = valueCopy
		closer.Close()
	}
	if err == pebble.ErrNotFound || len(value) == 0 {
		return nil, nil
	}
	return value, err
}

func (db *Store) Put(key, value []byte) error {
	if len(key) == 0 {
		return errors.New("empty key")
	}
	return db.rdb.Set(key, value, pebble.NoSync)
}

func (db *Store) Delete(key []byte) error {
	if len(key) == 0 {
		return errors.New("empty key")
	}
	return db.rdb.Delete(key, pebble.NoSync)
}

// func (db *Store) Iterator(key []byte) *rocksdb.Iterator {
func (db *Store) Iterator() *iterator {
	opts := &pebble.IterOptions{}
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

func (db *Store) TimeTravelReverseId(t time.Time) flake.Id {
	duration := flake.MaxTime.Sub(t)
	reverseTime := time.Unix(int64(duration.Seconds()), 0)
	gen := flake.NewGeneratorFromTime(reverseTime)
	fid, _ := gen.NextId()
	return fid
}

// All keys should partitioned by 4bytes table prefix
//
// Key interface

type Key interface {
	Prefix() Key
	Bytes() []byte
	String() string
	Len() int
}

// PrefixTable
func (p PrefixTable) Bytes() []byte {
	buf := make([]byte, p.Len())
	binary.BigEndian.PutUint32(buf, uint32(p))
	return buf
}

func (p PrefixTable) Len() int {
	return int(unsafe.Sizeof(p))
}

// Exists for satisfying Key interface
func (p PrefixTable) Prefix() Key {
	return p
}

func (p PrefixTable) String() string {
	return hex.EncodeToString(p.Bytes())
}

// --------------------------------------------------
//
// Meta key, used to store meta info.
//
// Defined as following:
// +----------+----------+
// |  4bytes  |   ?bytes |
// +----------+----------+
// |  table   |  string  |
// +----------+----------+
type MetaKey struct {
	PrefixTable
	Meta string
}

func NewMetaKey(prefix PrefixTable, meta string) *MetaKey {
	return &MetaKey{prefix, meta}
}

func (k *MetaKey) Bytes() []byte {
	var preBytes [4]byte
	binary.BigEndian.PutUint32(preBytes[:], uint32(k.PrefixTable))

	var buf bytes.Buffer
	buf.Write(preBytes[:])
	buf.Write([]byte(k.Meta))
	return buf.Bytes()
}

func (k *MetaKey) Len() int {
	return k.PrefixTable.Len() + len(k.Meta)
}

func (k *MetaKey) Prefix() Key {
	return k.PrefixTable
}

func (k *MetaKey) String() string {
	return hex.EncodeToString(k.Prefix().Bytes()) + k.Meta
}

// Defined as following:
// +----------+----------+
// |  4bytes  |  16bytes |
// +----------+----------+
// |  table   | flake id |
// +----------+----------+
type FlakeKey struct {
	PrefixTable
	Id flake.Id
}

func NewFlakeKey(prefix PrefixTable, id flake.Id) *FlakeKey {
	return &FlakeKey{prefix, id}
}

func (k *FlakeKey) Bytes() []byte {
	buf := new(bytes.Buffer)
	// nothing we can do if cannot allocate memory
	if err := binary.Write(buf, binary.BigEndian, k.PrefixTable); err != nil {
		panic(err)
	}
	if err := binary.Write(buf, binary.BigEndian, k.Id); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func (k *FlakeKey) Len() int {
	return k.PrefixTable.Len() + len(k.Id)
}

func (k *FlakeKey) Prefix() Key {
	return k.PrefixTable
}

func (k *FlakeKey) String() string {
	return hex.EncodeToString(k.Bytes())
}

// UUID Key.
//
// +----------+----------+
// |  4bytes  |  16bytes |
// +----------+----------+
// |  table   |   uuid   |
// +----------+----------+
type UUIDKey struct {
	PrefixTable
	uuid uuid.UUID //[16]byte
}

func NewUUIDKey(prefix PrefixTable, id uuid.UUID) *UUIDKey {
	return &UUIDKey{prefix, id}
}

func (k *UUIDKey) Bytes() []byte {
	var buf bytes.Buffer
	var tb [4]byte
	binary.BigEndian.PutUint32(tb[:], uint32(k.PrefixTable))
	buf.Write(tb[:])
	buf.Write(k.uuid[:])
	return buf.Bytes()
}

func (k *UUIDKey) Len() int {
	return int(unsafe.Sizeof(k.uuid)) + k.Prefix().Len()
}

func (k *UUIDKey) Prefix() Key {
	return k.PrefixTable
}

func (k *UUIDKey) String() string {
	return hex.EncodeToString(k.Bytes())
}

// UUID Flake Key.
//
// +----------+----------+----------+
// |  4bytes  |  16bytes |  16bytes |
// +----------+----------+----------+
// |  table   |   uuid   | flake id |
// +----------+----------+----------+
type UUIDFlakeKey struct {
	UUIDKey
	Id flake.Id
}

func NewUUIDFlakeKey(prefix PrefixTable, uuid uuid.UUID, id flake.Id) *UUIDFlakeKey {
	uk := UUIDKey{prefix, uuid}
	return &UUIDFlakeKey{uk, id}
}

func (k *UUIDFlakeKey) Bytes() []byte {
	var preBytes [4]byte
	binary.BigEndian.PutUint32(preBytes[:], uint32(k.PrefixTable))

	var buf bytes.Buffer
	buf.Write(preBytes[:])
	buf.Write(k.uuid[:])
	buf.Write(k.Id[:])
	return buf.Bytes()
}

func (k *UUIDFlakeKey) Len() int {
	return k.UUIDKey.Len() + int(unsafe.Sizeof(k.Id))
}

func (k *UUIDFlakeKey) Prefix() Key {
	return &k.UUIDKey
}

func (k *UUIDFlakeKey) String() string {
	return hex.EncodeToString(k.Bytes())
}
