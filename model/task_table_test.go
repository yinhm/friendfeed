package model

import "testing"

func TestTaskTablePrefixes(t *testing.T) {
	tables := []struct {
		name string
		got  []byte
		want []byte
	}{
		{"Task", Task.Prefix, KeyPrefixToBytes(TableTask)},
		{"TaskReady", TaskReady.Prefix, KeyPrefixToBytes(TableTaskReady)},
		{"TaskLease", TaskLease.Prefix, KeyPrefixToBytes(TableTaskLease)},
		{"TaskIdem", TaskIdem.Prefix, KeyPrefixToBytes(TableTaskIdem)},
		{"TaskDone", TaskDone.Prefix, KeyPrefixToBytes(TableTaskDone)},
		{"FeedService", FeedService.Prefix, KeyPrefixToBytes(TableFeedService)},
		{"Service", Service.Prefix, KeyPrefixToBytes(TableService)},
		{"ServiceState", ServiceState.Prefix, KeyPrefixToBytes(TableServiceState)},
		{"ServiceFeedIndex", ServiceFeedIndex.Prefix, KeyPrefixToBytes(TableServiceFeedIndex)},
	}
	for _, table := range tables {
		if string(table.got) != string(table.want) {
			t.Errorf("%s prefix = %x; want %x", table.name, table.got, table.want)
		}
	}
}
