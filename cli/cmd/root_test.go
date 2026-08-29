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
	if err := os.WriteFile(configPath, []byte(`{"media_path":"/media-from-file"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIA_PATH", "/media-from-env")

	initConfig()

	if got := viper.ConfigFileUsed(); got != configPath {
		t.Fatalf("ConfigFileUsed() = %q; want %q", got, configPath)
	}
	if got := viper.GetString("media_path"); got != "/media-from-env" {
		t.Fatalf("media_path = %q; want /media-from-env", got)
	}
}
