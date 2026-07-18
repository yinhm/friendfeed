package util

import (
	"path/filepath"
	"testing"
)

func TestNewConfigFromJSONReturnsReadError(t *testing.T) {
	config, err := NewConfigFromJSON(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected an error for a missing config file")
	}
	if config != nil {
		t.Fatalf("expected no config on read error, got %#v", config)
	}
}
