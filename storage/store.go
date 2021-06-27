package store

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"log"
	"time"
	"unsafe"

	"github.com/golang/glog"
	uuid "github.com/satori/go.uuid"
	rocksdb "github.com/tecbot/gorocksdb"
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
	rdb     *rocksdb.DB
	options *rocksdb.Options
	ro      *rocksdb.ReadOptions
	wo      *rocksdb.WriteOptions

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
	db.initReadOptions()
	db.initWriteOptions()

	rdb, err := rocksdb.OpenDb(db.options, db.dbpath)
	if err != nil {
		glog.Errorf("Can not open db: %s", err)
		err = rocksdb.RepairDb(db.dbpath, db.options)
		if err != nil {
			glog.Fatalf("Can not repair: %s", err)
		}
		glog.Fatalf("Repair success, please re-run.")
	}
	db.rdb = rdb

	idGen := flake.NewGenerator()
	db.idGen = idGen
	db.closed = false

	return db
}

func NewMetaStore(dbpath string) *Store {
	db := new(Store)
	db.dbpath = dbpath
	db.options = NewMetaStoreOptions()
	db.initReadOptions()
	db.initWriteOptions()

	rdb, err := rocksdb.OpenDb(db.options, db.dbpath)
	if err != nil {
		glog.Errorf("Can not open db: %s", err)
		err = rocksdb.RepairDb(db.dbpath, db.options)
		if err != nil {
			glog.Fatalf("Can not repair: %s", err)
		}
		glog.Fatalf("Repair success, please re-run.")
	}
	db.rdb = rdb

	idGen := flake.NewGenerator()
	db.idGen = idGen

	return db
}

func DestroyStore(dbpath string, options *rocksdb.Options) error {
	return rocksdb.DestroyDb(dbpath, options)
}

func NewStoreOptions() *rocksdb.Options {
	var prefix UUIDKey
	transform := rocksdb.NewFixedPrefixTransform(prefix.Len())

	opts := rocksdb.NewDefaultOptions()
	opts.SetPrefixExtractor(transform)
	opts.SetWriteBufferSize(64 * 1024 * 1024) // 64MB
	opts.SetTargetFileSizeBase(64 * 1024 * 1024)
	opts.SetMaxOpenFiles(10 * 10000)
	opts.SetMaxWriteBufferNumber(3)
	opts.SetCreateIfMissing(true)

	b := rocksdb.NewDefaultBlockBasedTableOptions()
	b.SetBlockCache(rocksdb.NewLRUCache(1024 * 1024 * 1024)) // 1GB
	// b.SetBlockCacheCompressed(rocksdb.NewLRUCache(128 * 1024 * 1024))
	// Default bits_per_key is 10, which yields ~1% false positive rate.
	b.SetFilterPolicy(rocksdb.NewBloomFilter(10))
	opts.SetBlockBasedTableFactory(b)
	return opts
}

func NewMetaStoreOptions() *rocksdb.Options {
	var prefix PrefixTable
	transform := rocksdb.NewFixedPrefixTransform(prefix.Len())

	opts := rocksdb.NewDefaultOptions()
	opts.SetPrefixExtractor(transform)
	opts.SetWriteBufferSize(64 * 1024 * 1024) // 64MB
	opts.SetTargetFileSizeBase(64 * 1024 * 1024)
	opts.SetMaxOpenFiles(5 * 10000)
	opts.SetMaxWriteBufferNumber(3)
	opts.SetCreateIfMissing(true)

	b := rocksdb.NewDefaultBlockBasedTableOptions()
	b.SetBlockCache(rocksdb.NewLRUCache(1014 * 1024 * 1024)) // 1GB
	// b.SetBlockCacheCompressed(rocksdb.NewLRUCache(128 * 1024 * 1024))
	// Default bits_per_key is 10, which yields ~1% false positive rate.
	b.SetFilterPolicy(rocksdb.NewBloomFilter(10))
	opts.SetBlockBasedTableFactory(b)
	return opts
}

func (db *Store) initReadOptions() {
	db.ro = rocksdb.NewDefaultReadOptions()
}

func (db *Store) initWriteOptions() {
	db.wo = rocksdb.NewDefaultWriteOptions()
}

func (db *Store) Close() {
	db.rdb.Close()
	db.closed = true
}

func (db *Store) Destroy() error {
	// log.Printf("WARN: destroy path %s", db.dbpath)
	return rocksdb.DestroyDb(db.dbpath, db.options)
}

func (db *Store) Options() *rocksdb.Options {
	return db.options
}

func (db *Store) Get(key []byte) ([]byte, error) {
	return db.rdb.GetBytes(db.ro, key)
}

func (db *Store) Put(key, value []byte) error {
	return db.rdb.Put(db.wo, key, value)
}

func (db *Store) Delete(key []byte) error {
	return db.rdb.Delete(db.wo, key)
}

// func (db *Store) Iterator(key []byte) *rocksdb.Iterator {
func (db *Store) Iterator() *rocksdb.Iterator {
	return db.rdb.NewIterator(db.ro)
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
