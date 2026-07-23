package store

import (
	"github.com/cockroachdb/pebble/v2"
	"google.golang.org/protobuf/proto"
)

// iterator is a wrapper around a pebble.Iterator
type Iterator struct {
	// Underlying iterator for the DB.
	iter *pebble.Iterator
}

func KeyUpperBound(b []byte) []byte {
	end := make([]byte, len(b))
	copy(end, b)
	for i := len(end) - 1; i >= 0; i-- {
		end[i] = end[i] + 1
		if end[i] != 0 {
			return end[:i+1]
		}
	}
	return nil // no upper-bound
}

func PrefixIteratorOptions(prefix []byte) *pebble.IterOptions {
	return &pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: KeyUpperBound(prefix),
	}
}

// Instantiates a new Pebble iterator wrapper
func newIterator(db pebble.Reader, opts *pebble.IterOptions) *Iterator {
	p := &Iterator{}

	// old version pebble.Reader.NewIter does not return err
	iter, err := db.NewIter(opts)
	if iter == nil || err != nil {
		panic("unable to create iterator")
	}
	p.iter = iter
	return p
}

func (p *Iterator) Next() {
	p.iter.Next()
}

func (p *Iterator) Prev() {
	p.iter.Prev()
}

func (p *Iterator) SeekGE(key []byte) {
	p.iter.SeekGE(key)
}

func (p *Iterator) SeekLT(key []byte) {
	p.iter.SeekLT(key)
}

func (p *Iterator) First() bool {
	return p.iter.First()
}

func (p *Iterator) Last() {
	p.iter.Last()
}

func (p *Iterator) UnsafeKey() Key {
	if !p.Valid() {
		return Key{}
	}
	return p.iter.Key()
}

// UnsafeRawKey returns the raw key from the underlying pebble.Iterator.
func (p *Iterator) UnsafeRawKey() []byte {
	return p.iter.Key()
}

func (p *Iterator) Valid() bool {
	return p.iter.Valid()
}

func (p *Iterator) Error() error {
	return p.iter.Error()
}

func (p *Iterator) Key() Key {
	key := p.UnsafeKey()
	return key.Copy()
}

func (p *Iterator) UnsafeValue() []byte {
	if !p.Valid() {
		return nil
	}
	return p.iter.Value()
}

func (p *Iterator) Value() []byte {
	value := p.UnsafeValue()
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	return valueCopy
}

func (p *Iterator) ValueProto(msg proto.Message) error {
	raw := p.UnsafeValue()
	return proto.Unmarshal(raw, msg)
}

func (p *Iterator) Close() error {
	return p.iter.Close()
}
