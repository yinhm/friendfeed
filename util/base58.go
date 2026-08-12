package util

import (
	"errors"
	"fmt"
)

// Base58 uses the Bitcoin alphabet: no 0/O/I/l lookalikes and no URL-unsafe
// punctuation, so encoded values pass through links and query strings intact.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var base58Table [128]int8

func init() {
	for i := range base58Table {
		base58Table[i] = -1
	}
	for i := 0; i < len(base58Alphabet); i++ {
		base58Table[base58Alphabet[i]] = int8(i)
	}
}

// Base58Encode encodes binary data, preserving leading zero bytes as '1's.
func Base58Encode(input []byte) string {
	if len(input) == 0 {
		return ""
	}

	zeros := 0
	for zeros < len(input) && input[zeros] == 0 {
		zeros++
	}

	// log(256)/log(58) < 1.38 digits per byte
	size := (len(input)-zeros)*138/100 + 1
	buf := make([]byte, size)
	length := 0
	for _, b := range input[zeros:] {
		carry := int(b)
		i := 0
		for j := size - 1; (carry != 0 || i < length) && j >= 0; j, i = j-1, i+1 {
			carry += 256 * int(buf[j])
			buf[j] = byte(carry % 58)
			carry /= 58
		}
		length = i
	}

	start := size - length
	for start < size && buf[start] == 0 {
		start++
	}
	out := make([]byte, zeros+size-start)
	for i := range out[:zeros] {
		out[i] = '1'
	}
	for i, digit := range buf[start:] {
		out[zeros+i] = base58Alphabet[digit]
	}
	return string(out)
}

// Base58Decode reverses Base58Encode, rejecting characters outside the alphabet.
func Base58Decode(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}

	zeros := 0
	for zeros < len(s) && s[zeros] == '1' {
		zeros++
	}

	// log(58)/log(256) < 0.733 bytes per digit
	size := (len(s)-zeros)*733/1000 + 1
	buf := make([]byte, size)
	length := 0
	for i := zeros; i < len(s); i++ {
		c := s[i]
		if c >= 128 || base58Table[c] < 0 {
			return nil, fmt.Errorf("invalid base58 character %q", c)
		}
		carry := int(base58Table[c])
		j := 0
		for k := size - 1; (carry != 0 || j < length) && k >= 0; k, j = k-1, j+1 {
			carry += 58 * int(buf[k])
			buf[k] = byte(carry % 256)
			carry /= 256
		}
		if carry != 0 {
			return nil, errors.New("base58 value overflows decoded size")
		}
		length = j
	}

	start := size - length
	for start < size && buf[start] == 0 {
		start++
	}
	out := make([]byte, zeros+size-start)
	copy(out[zeros:], buf[start:])
	return out, nil
}
