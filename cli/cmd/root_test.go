package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestInitConfigReadsExplicitFileAndEnvironment(t *testing.T) {
	oldDataPath := config.datapath
	viper.Reset()
	t.Cleanup(func() {
		config.datapath = oldDataPath
		viper.Reset()
	})

	config.datapath = t.TempDir()
	configPath := filepath.Join(config.datapath, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"twitter_api_key":"file-key","media_path":"/media"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TWITTER_API_SECRET", "env-secret")

	initConfig()

	if got := viper.ConfigFileUsed(); got != configPath {
		t.Fatalf("ConfigFileUsed() = %q; want %q", got, configPath)
	}
	if got := viper.GetString("twitter_api_key"); got != "file-key" {
		t.Fatalf("twitter_api_key = %q; want file-key", got)
	}
	if got := viper.GetString("twitter_api_secret"); got != "env-secret" {
		t.Fatalf("twitter_api_secret = %q; want env-secret", got)
	}
	if got := viper.GetString("media_path"); got != "/media" {
		t.Fatalf("media_path = %q; want /media", got)
	}
}
