package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestWarnDeprecatedDebugFlagOnlyWhenExplicit(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	var debug bool
	cmd.Flags().BoolVar(&debug, "debug", false, "")
	out := new(bytes.Buffer)
	cmd.SetErr(out)

	warnDeprecatedFlags(cmd)
	if out.Len() != 0 {
		t.Fatalf("warning without explicit flag: %q", out.String())
	}
	if err := cmd.Flags().Set("debug", "true"); err != nil {
		t.Fatal(err)
	}
	warnDeprecatedFlags(cmd)
	if got := out.String(); got != "warning: --debug is deprecated and has no effect; it will be removed in v2.3\n" {
		t.Fatalf("warning = %q", got)
	}
}

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
