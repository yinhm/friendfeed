// Falke: k-ordered id generation[1][2].
//
// Snowflake use 41bits timestamp(delta to TwitterEpoch) fix the max year to
// 2079, which gives us a hard time on reverse search(rocksdb related), hence
// we migrate to full falke implementation.
//
// Original Go implementation from alindeman[3], all credits goes to the
// author.
//
// What's a k-ordered ID?
//
// Crystal produces 128 bit k-ordered IDs. The first 64 bits is a timestamp, so
// IDs are time-ordered lexically.
//
// The next 48 bits is a unique worker ID, usually set to the MAC address of a
// machine's network interface, so IDs are conflict-free but without coordination.
// You should run crystal locally on every node that needs to generate IDs.
//
// The final 16 bits is a sequence ID, incremented each time an ID is generated in
// the same millisecond as a previous ID. The sequence is reset to 0 when time
// advances.
//
// |--------------------------------|-------------------------|--------|
// |              64                |            48           |   16   |
// |          Timestamp (ms)        |   Worker ID (MAC addr)  |  Seqn  |
// |--------------------------------|-------------------------|--------|
//
// [1] http://www.boundary.com/blog/2012/01/flake-a-decentralized-k-ordered-unique-id-generator-in-erlang/
// [2] https://github.com/boundary/flake
// [3] https://github.com/alindeman/crystal

package flake

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"time"
)

var (
	// Clock is moving backwards.
	ErrClockMovingBackwards = errors.New("system clock is moving backwards")

	// MaxTime - timestamp for build reverse index
	MaxTime = time.Date(2254, 6, 4, 00, 0, 0, 0, time.UTC)
)

// A `TimeSource` returns the current time. `time.Now()` fits the bill, for
// example.
type TimeSource func() time.Time

// An Id is a 128 bit wide value. All values are encoded with big endian.
//
// * The first 64 bits encode milliseconds since unix epoch.
// * The next 48 bits encode the worker identifier, usually the MAC address of
//   the machine that generated the Id.
// * The final 16 bits encode a sequence to differentiate Ids generated in the
//   same millisecond.
type Id [16]byte

// A Worker ID is a 48 bit wide value, usually the MAC address of the machine
// that is generating IDs.
type WorkerId [6]byte

type Generator struct {
	sync.Mutex

	TimeSource  TimeSource
	CurrentTime time.Time
	WorkerId    WorkerId
	Sequence    uint16
}

func NewGenerator() *Generator {
	return &Generator{
		TimeSource: time.Now,
		WorkerId:   NewWorkerId(),
	}
}

func NewGeneratorFromTime(t time.Time) *Generator {
	timeTravel := func() time.Time { return t }
	return &Generator{
		TimeSource: timeTravel,
		WorkerId:   NewWorkerId(),
	}
}

func (gen *Generator) Timestamp() time.Time {
	return gen.TimeSource().UTC()
}

func (gen *Generator) NextId() (id Id, err error) {
	gen.Lock()
	defer gen.Unlock()

	ts := gen.Timestamp()
	genTimeMs := uint64(gen.CurrentTime.UnixNano() / 1e6)
	currentTimeMs := uint64(ts.UnixNano() / 1e6)
	if gen.CurrentTime.IsZero() || currentTimeMs > genTimeMs {
		gen.CurrentTime = ts
		gen.Sequence = 0
	} else if currentTimeMs == genTimeMs {
		gen.Sequence++
	} else {
		return id, ErrClockMovingBackwards
	}

	// Timestamp (64 bits)
	binary.BigEndian.PutUint64(id[0:8], uint64(ts.UnixNano()/1e6))
	// Worker ID (48 bits)
	copy(id[8:14], gen.WorkerId[:])
	// Sequence (16 bits)
	binary.BigEndian.PutUint16(id[14:16], gen.Sequence)

	return id, nil
}

// make it variable so we can test
var NewWorkerId = func() (id WorkerId) {
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if len(iface.HardwareAddr) == 0 {
				// skip loopback
				continue
			}
			copy(id[:], iface.HardwareAddr[0:len(id)])
			return id
		}
	}
	return NewRandWorkerId()
}

func NewRandWorkerId() (id WorkerId) {
	bytes := make([]byte, len(id))
	cryptorand.Read(bytes)
	copy(id[:], bytes[0:len(id)])
	return id
}

// Reconstructs a timestamp from the first 64 bits of an Id
func ParseTimestamp(id Id) time.Time {
	msSinceEpoch := int64(binary.BigEndian.Uint64(id[0:8]))
	return time.Unix(msSinceEpoch/1e3, (msSinceEpoch%1e3)*1e6)
}

// Reconstructs a timestamp from the first 64 bits of an Id
func ParseReverseTimestamp(id Id) time.Time {
	ms := int64(binary.BigEndian.Uint64(id[0:8]))
	reverseTime := time.Unix(ms/1e3, 0)
	msSinceEpoch := int64(MaxTime.Sub(reverseTime).Seconds())
	return time.Unix(msSinceEpoch, 0)
}
