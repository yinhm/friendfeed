package server

import (
	"net/http/httptest"
	"testing"
)

func TestParseStart(t *testing.T) {
	tests := []struct {
		rawQuery string
		want     int
	}{
		{"", 0},
		{"start=30", 30},
		// A negative offset must normalize to 0 so feedContext computes the
		// same next_start as the server-side clamp.
		{"start=-1", 0},
		{"start=20001", 20000},
		{"start=abc", 0},
	}
	for _, test := range tests {
		r := httptest.NewRequest("GET", "/search?"+test.rawQuery, nil)
		if got := ParseStart(r); got != test.want {
			t.Fatalf("ParseStart(%q) = %d; want %d", test.rawQuery, got, test.want)
		}
	}
}
