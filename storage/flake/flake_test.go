package flake

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestTimestampIsFirst64Bits(t *testing.T) {
	ts := time.Unix(12345689, 200*1e6)
	generator := &Generator{
		TimeSource: func() time.Time { return ts },
	}

	id, err := generator.NextId()
	if err != nil {
		t.Fatal(err)
	}

	idTimestamp := ParseTimestamp(id)
	if idTimestamp != ts {
		t.Fatalf("Expected timestamp in ID to be %v, but was %v", ts, idTimestamp)
	}
}

func TestWorkerIdIsNext48Bits(t *testing.T) {
	workerId := WorkerId{1, 2, 3, 4, 5, 6}
	generator := &Generator{
		TimeSource: time.Now,
		WorkerId:   workerId,
	}

	id, err := generator.NextId()
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Compare(id[8:14], workerId[:]) != 0 {
		t.Fatalf("Expected worker ID in ID to be %v, but was %v", workerId, id[8:14])
	}
}

func TestSequenceIsFinal16Bits(t *testing.T) {
	sequence := uint16(1234)
	ts := time.Unix(12345689, 200*1e6)
	generator := &Generator{
		CurrentTime: ts,
		TimeSource:  func() time.Time { return ts },
		Sequence:    sequence,
	}

	sequenceBytes := make([]byte, 2)
	// the sequence will be incremented by one before an ID is generated
	binary.BigEndian.PutUint16(sequenceBytes, sequence+1)

	id, err := generator.NextId()
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Compare(id[14:16], sequenceBytes) != 0 {
		t.Fatalf("Expected sequence in ID to be %v, but was %v", sequenceBytes, id[14:16])
	}
}

func TestSequenceIsIncrementedForSameTimestamp(t *testing.T) {
	ts := time.Unix(12345689, 200*1e6)
	generator := &Generator{
		TimeSource: func() time.Time { return ts },
	}

	id, err := generator.NextId()
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Compare(id[14:16], []byte{0, 0}) != 0 {
		t.Fatalf("Expected sequence in ID to be [0 0] on the first run, but was %v", id[14:16])
	}

	id2, err := generator.NextId()
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Compare(id2[14:16], []byte{0, 1}) != 0 {
		t.Fatalf("Expected sequence in ID to be [0 1] on the second run, but was %v", id2[14:16])
	}
}

func TestSequenceIsResetWhenTimeMovesForward(t *testing.T) {
	ts := time.Unix(123456789, 200*1e6)
	generator := &Generator{
		CurrentTime: ts,
		TimeSource: func() time.Time {
			ts = ts.Add(time.Millisecond) // each invocation, increment timestamp
			return ts
		},
	}

	id, err := generator.NextId()
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Compare(id[14:16], []byte{0, 0}) != 0 {
		t.Fatalf("Expected sequence in ID to be [0 0] on the first run, but was %v", id[14:16])
	}

	id2, err := generator.NextId()
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Compare(id2[14:16], []byte{0, 0}) != 0 {
		t.Fatalf("Expected sequence in ID to be [0 0] on the second run, but was %v", id2[14:16])
	}
}

func TestClockRunningBackwardsIsAnError(t *testing.T) {
	ts := time.Unix(123456789, 200*1e6)
	generator := &Generator{
		CurrentTime: ts,
		TimeSource:  func() time.Time { return ts.Add(-time.Millisecond) },
	}

	_, err := generator.NextId()
	if err != ErrClockMovingBackwards {
		t.Fatalf("Expected a ClockRunningBackward error, but was %v", err)
	}
}

func TestNewWorkerId(t *testing.T) {
	Convey("Random worker id", t, func() {
		for i := 0; i < 10; i++ {
			id1 := NewWorkerId()
			id2 := NewWorkerId()
			So(hex.EncodeToString(id1[:]), ShouldEqual, hex.EncodeToString(id2[:]))
		}
	})
}

func TestNewRandWorkerId(t *testing.T) {
	Convey("Random worker id", t, func() {
		for i := 0; i < 100; i++ {
			id1 := NewRandWorkerId()
			id2 := NewRandWorkerId()
			So(hex.EncodeToString(id1[:]), ShouldNotEqual, hex.EncodeToString(id2[:]))
		}
	})
}

func TestTimeTravel(t *testing.T) {
	Convey("Parse timestamp from ID", t, func() {
		tSlice := []time.Time{
			// unix time start from January 1, 1970 UTC
			time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(1974, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(1984, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2005, 9, 10, 23, 0, 0, 0, time.UTC),
			time.Date(2015, 9, 10, 23, 0, 0, 0, time.UTC),
			time.Date(2025, 4, 5, 23, 0, 0, 0, time.UTC),
			time.Date(2035, 4, 5, 23, 0, 0, 0, time.UTC),
			time.Date(2045, 4, 5, 23, 0, 0, 0, time.UTC),
			time.Date(2145, 4, 5, 23, 0, 0, 0, time.UTC),
			time.Date(2245, 4, 5, 23, 0, 0, 0, time.UTC),
			time.Date(2254, 6, 4, 00, 0, 0, 0, time.UTC),
		}

		for _, expect := range tSlice {
			gen := NewGeneratorFromTime(expect)
			id, _ := gen.NextId()
			got := ParseTimestamp(id)
			So(got.Equal(expect), ShouldBeTrue)
		}
	})
}

func TestTimeReverseTravel(t *testing.T) {
	Convey("Parse timestamp from ID", t, func() {
		tSlice := []time.Time{
			// unix time start from January 1, 1970 UTC
			time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(1984, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2005, 9, 10, 23, 0, 0, 0, time.UTC),
			time.Date(2015, 9, 10, 23, 0, 0, 0, time.UTC),
			time.Date(2025, 4, 5, 23, 0, 0, 0, time.UTC),
			time.Date(2035, 4, 5, 23, 0, 0, 0, time.UTC),
			time.Date(2045, 4, 5, 23, 0, 0, 0, time.UTC),
			time.Date(2145, 4, 5, 23, 0, 0, 0, time.UTC),
			time.Date(2245, 4, 5, 23, 0, 0, 0, time.UTC),
			time.Date(2254, 6, 4, 00, 0, 0, 0, time.UTC),
		}

		for _, expect := range tSlice {
			// shift := MaxTime.Unix() - expect.Unix()
			shift := MaxTime.Sub(expect)
			reverseTime := time.Unix(int64(shift.Seconds()), 0)
			gen := NewGeneratorFromTime(reverseTime)
			id, _ := gen.NextId()
			got := ParseReverseTimestamp(id)
			So(got.Equal(expect), ShouldBeTrue)
		}
	})
}
