package store

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestTimeParse(t *testing.T) {
	Convey("Given RFC3339, parse time string", t, func() {
		dt := "2009-06-25T18:23:38Z"
		got, _ := time.Parse(time.RFC3339, dt)
		So(got.Year(), ShouldEqual, 2009)
	})
}
