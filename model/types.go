package model

import (
	"log"

	"github.com/golang/protobuf/proto"
	store "github.com/yinhm/friendfeed/storage"
)

var (
	tableInited bool

	Config *Table
	Topic  *Table
	Stock  *Table
)

const (
	TableConfig store.KeyPrefix = 300
	TableTopic  store.KeyPrefix = 301
	TableStock  store.KeyPrefix = 302
)

func init() {
	Config = NewTable(KeyPrefixToBytes(TableConfig))
	Topic = NewTable(KeyPrefixToBytes(TableTopic))
	Stock = NewTable(KeyPrefixToBytes(TableStock))
}

func InitTables(db *store.Store) {
	if tableInited {
		log.Fatalf("table inited")
	}
	Config.InitStore(db)
	Topic.InitStore(db)
	Stock.InitStore(db)
	tableInited = true
}

type ProtoMessageFunc func() proto.Message
