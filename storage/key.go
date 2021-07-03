package store

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
)

var (
	// KeyMin is a minimum key value which sorts before all other keys.
	KeyMin = Key("")
	// KeyMax is a maximum key value which sorts after all other keys.
	KeyMax = Key{0xff, 0xff}
)

type Key []byte

// Copy makes a copy of the key.
func (k Key) Copy() Key {
	buf := make([]byte, len(k))
	copy(buf, k)
	return buf
}

func (k Key) Len() int {
	return len(k)
}

func (k Key) String() string {
	return hex.EncodeToString(k)
}

func KeyFromString(k string) Key {
	b, err := hex.DecodeString(k)
	// FIXME: not all string are hex encoded
	// this leads to inconsistency
	if err != nil {
		return []byte(k)
	}
	return b
}

// BytesNext returns the next possible byte slice, using the extra capacity
// of the provided slice if possible, and if not, appending an \x00.
func BytesNext(b []byte) []byte {
	if cap(b) > len(b) {
		bNext := b[:len(b)+1]
		if bNext[len(bNext)-1] == 0 {
			return bNext
		}
	}
	// TODO(spencer): Do we need to enforce KeyMaxLength here?
	// Switched to "make and copy" pattern in #4963 for performance.
	bn := make([]byte, len(b)+1)
	copy(bn, b)
	bn[len(bn)-1] = 0
	return bn
}

func bytesPrefixEnd(b []byte) []byte {
	// Switched to "make and copy" pattern in #4963 for performance.
	end := make([]byte, len(b))
	copy(end, b)
	for i := len(end) - 1; i >= 0; i-- {
		end[i] = end[i] + 1
		if end[i] != 0 {
			return end[:i+1]
		}
	}
	// This statement will only be reached if the key is already a
	// maximal byte string (i.e. already \xff...).
	return b
}

// Next returns the next key in   sort order. The method may only
// take a shallow copy of the Key, so both the receiver and the return
// value should be treated as immutable after.
func (k Key) Next() Key {
	return Key(BytesNext(k))
}

// IsPrev is a more efficient version of k.Next().Equal(m).
func (k Key) IsPrev(m Key) bool {
	l := len(m) - 1
	return l == len(k) && m[l] == 0 && k.Equal(m[:l])
}

// PrefixEnd determines the end key given key as a prefix, that is the
// key that sorts precisely behind all keys starting with prefix: "1"
// is added to the final byte and the carry propagated. The special
// cases of nil and KeyMin always returns KeyMax.
func (k Key) PrefixEnd() Key {
	if len(k) == 0 {
		return Key(KeyMax)
	}
	return Key(bytesPrefixEnd(k))
}

// Equal returns whether two keys are identical.
func (k Key) Equal(l Key) bool {
	return bytes.Equal(k, l)
}

// Compare compares the two Keys.
func (k Key) Compare(b Key) int {
	return bytes.Compare(k, b)
}

// Format implements the fmt.Formatter interface.
func (k Key) Format(f fmt.State, verb rune) {
	// Note: this implementation doesn't handle the width and precision
	// specifiers such as "%20.10s".
	if verb == 'x' {
		fmt.Fprintf(f, "%x", []byte(k))
	} else {
		fmt.Fprint(f, strconv.Quote(string(k)))
	}
}
