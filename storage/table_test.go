package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTimeParse(t *testing.T) {
	// "Given RFC3339, parse time string"
	// Z           A suffix which, when applied to a time, denotes a UTC
	//             offset of 00:00; often spoken "Zulu" from the ICAO
	//             phonetic alphabet representation of the letter "Z".
	dt := "2009-06-25T18:23:38Z"
	got, _ := time.Parse(time.RFC3339, dt)

	assert.Equal(t, 2009, got.Year())
	assert.Equal(t, 18, got.Hour())
	assert.Equal(t, 18, got.UTC().Hour())
}

func TestTimeFormat(t *testing.T) {
	// Given time, format RFC3339 string
	dt := "2009-06-25T18:23:38Z"
	rfcTime, _ := time.Parse(time.RFC3339, dt)
	got := rfcTime.Format(time.RFC3339)
	assert.Equal(t, dt, got)
}
