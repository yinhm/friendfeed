package server

import (
	"fmt"
	"testing"

	uuid "github.com/satori/go.uuid"
	. "github.com/smartystreets/goconvey/convey"
)

func TestFeedIndex(t *testing.T) {
	Convey("Given feed index, push and rebuild", t, func() {
		uuid1 := "c6f8dca854f011ddb489003048343a40"
		index := NewFeedIndex("public", new(uuid.UUID))

		for i := 0; i < 10; i++ {
			// index.itemCh <- uuid
			index.Push(uuid1)
		}

		index.rebuild()
		So(len(index.bufq), ShouldEqual, MinQueue)
		So(index.bufq[0], ShouldEqual, "c6f8dca854f011ddb489003048343a40")
		for i := 1; i < len(index.bufq); i++ {
			So(index.bufq[i], ShouldEqual, "")
		}

		for i := 0; i < MinQueue; i++ {
			uuid1 := fmt.Sprintf("uuid-%d", i)
			index.Push(uuid1)
		}

		index.rebuild()
		for i := 0; i < len(index.bufq); i++ {
			So(index.bufq[i], ShouldNotEqual, "")
			So(index.bufq[i], ShouldNotEqual, "c6f8dca854f011ddb489003048343a40")
		}

		index.doneCh <- struct{}{}
	})
}
