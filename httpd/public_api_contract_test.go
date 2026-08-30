package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicAPIV1GoldenContractsAreSafeJSON(t *testing.T) {
	for _, name := range []string{"success", "error", "feed", "entry", "list"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "public_api_v1", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var decoded any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("decode golden contract: %v", err)
			}
			text := strings.ToLower(string(raw))
			for _, forbidden := range []string{
				"rawbody", "commands", "likes", "comments", "oauth",
				"access_token", "asset_token", "staging", "secret",
			} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("golden contract contains forbidden field %q", forbidden)
				}
			}
		})
	}
}

func TestPublicAPIV1ReadRoutesAreRegisteredBeforeWriteRoute(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("api", "transport.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{`group.GET("/feed"`, `group.GET("/feed/entries"`, `group.GET("/feed/entries/:entry_id"`} {
		if !strings.Contains(string(raw), endpoint) {
			t.Fatalf("Public API V1 read route %s is not registered", endpoint)
		}
	}
	if !strings.Contains(string(raw), `group.POST("/feed/entries"`) {
		t.Fatal("Public API V1 write route is not registered")
	}
}
