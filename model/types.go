package model

import (
	"errors"

	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

var ErrNotFound = errors.New("db: key not found or no value")

// FIX : use go generate
type ProtoMessageFunc func() proto.Message

const (
	TableMeta store.KeyPrefix = 0

	TableFeed     store.KeyPrefix = 1
	TableFeedinfo store.KeyPrefix = 2
	TableEntry    store.KeyPrefix = 3
	TableTweet    store.KeyPrefix = 6
	TableUserMap  store.KeyPrefix = 4
	// UserRenameMap maps a previous profile ID to the profile's stable UUID. It
	// is a small, periodically reclaimed metadata table used for soft redirects.
	TableUserRenameMap store.KeyPrefix = 7

	// Prev iter was broken on rocksdb when this was coded,
	// this index was actually a manually reverse index.
	// Don't know is that prev iter works well on pepple,
	// may change later?
	// TableEntryIndex store.KeyPrefix = 4
	TableEntryIndex store.KeyPrefix = 5

	TableProfile          store.KeyPrefix = 100
	TableFeedService      store.KeyPrefix = 101
	TableFollow           store.KeyPrefix = 102
	TableFollower         store.KeyPrefix = 103
	TableOAuth            store.KeyPrefix = 104
	TableFile             store.KeyPrefix = 105
	TableLike             store.KeyPrefix = 106
	TableComment          store.KeyPrefix = 107
	TableTimelineIndex    store.KeyPrefix = 108
	TableTimelinePosition store.KeyPrefix = 109
	TableTimelineState    store.KeyPrefix = 110
	TableService          store.KeyPrefix = 111
	TableServiceState     store.KeyPrefix = 112
	TableServiceFeedIndex store.KeyPrefix = 113

	TableJobFeed    store.KeyPrefix = 200
	TableJobRunning store.KeyPrefix = 201
	TableJobHistory store.KeyPrefix = 202
	TableTask       store.KeyPrefix = 203
	TableTaskReady  store.KeyPrefix = 204
	TableTaskLease  store.KeyPrefix = 205
	TableTaskIdem   store.KeyPrefix = 206
	TableTaskDone   store.KeyPrefix = 207

	TableConfig store.KeyPrefix = 300
	TableTopic  store.KeyPrefix = 301
	TableStock  store.KeyPrefix = 302
	TableKLine  store.KeyPrefix = 303

	TableMax store.KeyPrefix = 1e8
)

var (
	// init tables

	// Never used
	// Feed = NewTable(KeyPrefixToBytes(TableFeed))
	// TODO:
	// Feedinfo should be generated from Profile and Feed?
	Feedinfo      = NewTable(KeyPrefixToBytes(TableFeedinfo))
	Entry         = NewTable(KeyPrefixToBytes(TableEntry))
	Tweet         = NewTable(KeyPrefixToBytes(TableTweet))
	EntryIndex    = NewTable(KeyPrefixToBytes(TableEntryIndex))
	UserMap       = NewTable(KeyPrefixToBytes(TableUserMap))
	UserRenameMap = NewTable(KeyPrefixToBytes(TableUserRenameMap))
	Profile       = NewTable(KeyPrefixToBytes(TableProfile))

	FeedService      = NewTable(KeyPrefixToBytes(TableFeedService))
	Follow           = NewTable(KeyPrefixToBytes(TableFollow))
	Follower         = NewTable(KeyPrefixToBytes(TableFollower))
	OAuth            = NewTable(KeyPrefixToBytes(TableOAuth))
	File             = NewTable(KeyPrefixToBytes(TableFile))
	Like             = NewTable(KeyPrefixToBytes(TableLike))
	Comment          = NewTable(KeyPrefixToBytes(TableComment))
	TimelineIndex    = NewTable(KeyPrefixToBytes(TableTimelineIndex))
	TimelinePosition = NewTable(KeyPrefixToBytes(TableTimelinePosition))
	TimelineState    = NewTable(KeyPrefixToBytes(TableTimelineState))
	Service          = NewTable(KeyPrefixToBytes(TableService))
	ServiceState     = NewTable(KeyPrefixToBytes(TableServiceState))
	ServiceFeedIndex = NewTable(KeyPrefixToBytes(TableServiceFeedIndex))

	JobFeed    = NewTable(KeyPrefixToBytes(TableJobFeed))
	JobRunning = NewTable(KeyPrefixToBytes(TableJobRunning))
	JobHistory = NewTable(KeyPrefixToBytes(TableJobHistory))
	Task       = NewTable(KeyPrefixToBytes(TableTask))
	TaskReady  = NewTable(KeyPrefixToBytes(TableTaskReady))
	TaskLease  = NewTable(KeyPrefixToBytes(TableTaskLease))
	TaskIdem   = NewTable(KeyPrefixToBytes(TableTaskIdem))
	TaskDone   = NewTable(KeyPrefixToBytes(TableTaskDone))

	Config = NewTable(KeyPrefixToBytes(TableConfig))
	Topic  = NewTable(KeyPrefixToBytes(TableTopic))
	Stock  = NewTable(KeyPrefixToBytes(TableStock))
	KLine  = NewTable(KeyPrefixToBytes(TableKLine))
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
