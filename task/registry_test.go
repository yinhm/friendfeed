package task

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validDefinition() Definition {
	return Definition{
		MaxAttempts:   3,
		LeaseDuration: time.Minute,
		MaxLease:      10 * time.Minute,
		BackoffBase:   time.Second,
		BackoffCap:    time.Minute,
	}
}

func TestRegistryValidatesDefinitionsAtStartup(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Definition)
	}{
		{"zero attempts", func(d *Definition) { d.MaxAttempts = 0 }},
		{"zero lease", func(d *Definition) { d.LeaseDuration = 0 }},
		{"lease above max", func(d *Definition) { d.LeaseDuration = d.MaxLease + time.Second }},
		{"zero backoff", func(d *Definition) { d.BackoffBase = 0 }},
		{"base above cap", func(d *Definition) { d.BackoffBase = d.BackoffCap + time.Second }},
		{"negative payload limit", func(d *Definition) { d.MaxPayloadBytes = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition := validDefinition()
			tt.mutate(&definition)
			_, err := NewRegistry(map[string]Definition{"rss.fetch": definition})
			require.Error(t, err)
		})
	}
}

func TestRegistryValidatesPayload(t *testing.T) {
	definition := validDefinition()
	definition.MaxPayloadBytes = 3
	definition.ValidatePayload = func(payload []byte, version uint32) error {
		if version != 1 {
			return errors.New("unsupported version")
		}
		return nil
	}
	registry, err := NewRegistry(map[string]Definition{"rss.fetch": definition})
	require.NoError(t, err)
	_, err = registry.Validate("rss.fetch", []byte("abc"), 1)
	require.NoError(t, err)
	_, err = registry.Validate("rss.fetch", []byte("abcd"), 1)
	require.ErrorContains(t, err, "limit")
	_, err = registry.Validate("rss.fetch", nil, 2)
	require.ErrorContains(t, err, "unsupported version")
}
