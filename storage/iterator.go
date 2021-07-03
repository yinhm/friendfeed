package store

import (
	"github.com/cockroachdb/pebble"
	"github.com/golang/protobuf/proto"
)

// iterator is a wrapper around a pebble.Iterator
type iterator struct {
	// Underlying iterator for the DB.
	iter    *pebble.Iterator
	options pebble.IterOptions
}

func keyUpperBound(b []byte) []byte {
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

func prefixIteratorOptions(prefix []byte) *pebble.IterOptions {
	return &pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: keyUpperBound(prefix),
	}
}

// Instantiates a new Pebble iterator wrapper
func newIterator(db pebble.Reader, opts *pebble.IterOptions) *iterator {
	p := &iterator{
		options: *opts,
	}

	p.iter = db.NewIter(&p.options)
	if p.iter == nil {
		panic("unable to create iterator")
	}
	return p
}

func (p *iterator) Next() {
	p.iter.Next()
}

func (p *iterator) Prev() {
	p.iter.Prev()
}

func (p *iterator) SeekGE(key []byte) {
	p.iter.SeekGE(key)
}

func (p *iterator) SeekLT(key []byte) {
	p.iter.SeekLT(key)
}

func (p *iterator) First() {
	p.iter.First()
}

func (p *iterator) Last() {
	p.iter.Last()
}

func (p *iterator) UnsafeKey() Key {
	if !p.Valid() {
		return Key{}
	}
	return p.iter.Key()
}

// UnsafeRawKey returns the raw key from the underlying pebble.Iterator.
func (p *iterator) UnsafeRawKey() []byte {
	return p.iter.Key()
}

func (p *iterator) Valid() bool {
	return p.iter.Valid()
}

func (p *iterator) Error() error {
	return p.iter.Error()
}

func (p *iterator) Key() Key {
	key := p.UnsafeKey()
	return key.Copy()
}

func (p *iterator) UnsafeValue() []byte {
	if !p.Valid() {
		return nil
	}
	return p.iter.Value()
}

func (p *iterator) Value() []byte {
	value := p.UnsafeValue()
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	return valueCopy
}

func (p *iterator) ValueProto(msg proto.Message) error {
	raw := p.UnsafeValue()
	return proto.Unmarshal(raw, msg)
}

func (p *iterator) Close() error {
	return p.iter.Close()
}
