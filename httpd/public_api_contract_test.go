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

func TestPublicAPIV1RoutesRemainClosedAtBaseline(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"/api/v1`) {
		t.Fatal("Public API V1 route opened before its transport phase")
	}
}
