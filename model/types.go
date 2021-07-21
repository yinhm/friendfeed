package model

import (
	"errors"

	"github.com/golang/protobuf/proto"
	store "github.com/yinhm/friendfeed/storage"
)

var ErrNotFound = errors.New("db: key not found or no value")

// FIXME: use go generate
type ProtoMessageFunc func() proto.Message

const (
	TableFeed     store.KeyPrefix = 1
	TableFeedinfo store.KeyPrefix = 2
	TableEntry    store.KeyPrefix = 3

	// Prev iter was broken on rocksdb when this was coded,
	// this index was actually a manually reverse index.
	// Don't know is that prev iter works well on pepple,
	// may change later?
	// TableEntryIndex store.KeyPrefix = 4
	TableEntryIndex store.KeyPrefix = 5
	TableIndexCache store.KeyPrefix = 6

	TableProfile      store.KeyPrefix = 100
	TableService      store.KeyPrefix = 101
	TableSubscription store.KeyPrefix = 102
	TableSubscriber   store.KeyPrefix = 103
	TableOAuth        store.KeyPrefix = 104

	TableJobFeed    store.KeyPrefix = 200
	TableJobRunning store.KeyPrefix = 201
	TableJobHistory store.KeyPrefix = 202

	TableConfig store.KeyPrefix = 300
	TableTopic  store.KeyPrefix = 301
	TableStock  store.KeyPrefix = 302

	TableMax store.KeyPrefix = 1e8
)

var (
	// init tables
	// Never used
	// Feed = NewTable(KeyPrefixToBytes(TableFeed))
	// TODO:
	// Feedinfo should be generated from Profile and Feed?
	Feedinfo   = NewTable(KeyPrefixToBytes(TableFeedinfo))
	Entry      = NewTable(KeyPrefixToBytes(TableEntry))
	EntryIndex = NewTable(KeyPrefixToBytes(TableEntryIndex))
	// FIXME: dont need to define a table for cache
	IndexCache = NewTable(KeyPrefixToBytes(TableIndexCache))
	Profile    = NewTable(KeyPrefixToBytes(TableProfile))

	// TODO: rename to follow, follower
	Service      = NewTable(KeyPrefixToBytes(TableService))
	Subscription = NewTable(KeyPrefixToBytes(TableSubscription))
	Subscriber   = NewTable(KeyPrefixToBytes(TableSubscriber))
	OAuth        = NewTable(KeyPrefixToBytes(TableOAuth))

	JobFeed    = NewTable(KeyPrefixToBytes(TableJobFeed))
	JobRunning = NewTable(KeyPrefixToBytes(TableJobRunning))
	JobHistory = NewTable(KeyPrefixToBytes(TableJobHistory))

	Config = NewTable(KeyPrefixToBytes(TableConfig))
	Topic  = NewTable(KeyPrefixToBytes(TableTopic))
	Stock  = NewTable(KeyPrefixToBytes(TableStock))
)

// func InitTables(db *store.Store) {
// 	if _tableInited {
// 		log.Fatalf("table inited")
// 	}
// 	Config.InitStore(db)
// 	Topic.InitStore(db)
// 	Stock.InitStore(db)
// 	_tableInited = true
// }
