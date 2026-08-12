package util

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestBase58KnownVectors(t *testing.T) {
	vectors := []struct {
		decoded []byte
		encoded string
	}{
		{nil, ""},
		{[]byte{0}, "1"},
		{[]byte{0, 0, 1}, "112"},
		{[]byte{0, 0, 0, 0}, "1111"},
		{[]byte("hello world"), "StV1DL6CwTryKyV"},
	}
	for _, v := range vectors {
		if got := Base58Encode(v.decoded); got != v.encoded {
			t.Errorf("Base58Encode(%x) = %q, want %q", v.decoded, got, v.encoded)
		}
		got, err := Base58Decode(v.encoded)
		if err != nil {
			t.Fatalf("Base58Decode(%q) error: %v", v.encoded, err)
		}
		if !bytes.Equal(got, v.decoded) {
			t.Errorf("Base58Decode(%q) = %x, want %x", v.encoded, got, v.decoded)
		}
	}
}

func TestBase58RoundTrip(t *testing.T) {
	for i := 0; i < 100; i++ {
		b := make([]byte, 1+i%33)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		if i%3 == 0 {
			b[0] = 0 // exercise leading zero bytes
		}
		got, err := Base58Decode(Base58Encode(b))
		if err != nil {
			t.Fatalf("Base58Decode(Base58Encode(%x)) error: %v", b, err)
		}
		if !bytes.Equal(got, b) {
			t.Fatalf("round trip = %x, want %x", got, b)
		}
	}
}

func TestBase58RejectsInvalidCharacters(t *testing.T) {
	for _, s := range []string{"0", "O", "I", "l", "-", "_", "+", "/", "=", "abc def"} {
		if _, err := Base58Decode(s); err == nil {
			t.Errorf("Base58Decode(%q) should fail", s)
		}
	}
}
