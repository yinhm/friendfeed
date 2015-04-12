package store

import (
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	. "github.com/smartystreets/goconvey/convey"
	pb "github.com/yinhm/friendfeed/proto"
)

func TestTimeParse(t *testing.T) {
	Convey("Given RFC3339, parse time string", t, func() {
		// Z           A suffix which, when applied to a time, denotes a UTC
		//             offset of 00:00; often spoken "Zulu" from the ICAO
		//             phonetic alphabet representation of the letter "Z".
		dt := "2009-06-25T18:23:38Z"
		got, _ := time.Parse(time.RFC3339, dt)
		So(got.Year(), ShouldEqual, 2009)
		So(got.Hour(), ShouldEqual, 18)
		So(got.UTC().Hour(), ShouldEqual, 18)
	})
}

func TestTimeFormat(t *testing.T) {
	Convey("Given time, format RFC3339 string", t, func() {
		dt := "2009-06-25T18:23:38Z"
		rfcTime, _ := time.Parse(time.RFC3339, dt)
		got := rfcTime.Format(time.RFC3339)
		So(got, ShouldEqual, dt)
	})
}

func TestPutEntry(t *testing.T) {
	setup()
	defer teardown()

	p := &pb.Profile{
		Uuid: "c6f8dca854f011ddb489003048343a40",
		Id:   "yinhm",
		Name: "yinhm",
		Type: "user",
	}

	feed := &pb.Feed{
		Id:   "yinhm",
		Name: "yinhm",
		Type: "user",
	}

	e := &pb.Entry{
		Body:        "张无忌对张三丰说：“太师父，武当山的生活太寂寞了，只有清风和明月两个朋友能陪我玩。”张三丰叹了口气：“已经很不错啦，至少还有清风明月呢。想当年我在少林寺的时候，也是只有两个朋友，其中一个也叫清风……”“那另一个呢？”“叫心相印。”…",
		Id:          "e/2b43a9066074d120ed2e45494eea1797",
		Date:        "2012-09-07T07:40:22Z",
		Url:         "http://friendfeed.com/yinhm/2b43a906/rt-trojansj",
		From:        feed,
		ProfileUuid: "c6f8dca854f011ddb489003048343a40",
	}

	Convey("Put entry", t, func() {
		// fresh put
		_, err := PutEntry(rdb, e, false)
		So(err, ShouldBeNil)

		// put exists entry
		_, err = PutEntry(rdb, e, false)
		_, ok := err.(*Error)
		So(ok, ShouldBeTrue)

		// force put
		e.Id = "e/ab439960a83546c683fd989a40a68462"
		// fake new falkeid
		e.Date = "2013-09-07T07:40:22Z"

		_, err = PutEntry(rdb, e, false)
		So(err, ShouldBeNil)

		// force put exists entry
		_, err = PutEntry(rdb, e, true)
		So(err, ShouldBeNil)

		for i := 0; i < 100; i++ {
			_, err = PutEntry(rdb, e, true)
			So(err, ShouldBeNil)
		}

		uuid1, _ := uuid.FromString(p.Uuid)
		key := NewUUIDKey(TableReverseEntryIndex, uuid1)
		n, err := ForwardTableScan(rdb, key, func(i int, k, v []byte) error {
			return nil
		})
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 2)
	})
}
