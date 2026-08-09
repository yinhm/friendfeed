package util

import (
	"encoding/json"
	"os"
)

// ----------------------------
// Server Config
// ----------------------------
type Config struct {
	Address string `json:"address"`
	DBPath  string `json:"db_path"`
	Debug   bool   `json:"debug"`

	ServerDomain string `json:"server_domain"`
	// MediaPath is the local directory mirrored media objects are written
	// to. Empty defaults to <db_path>/files (see media.NewLocalStorage).
	MediaPath string `json:"media_path"`
	// MediaURL is the base URL mirrored media objects are served from
	// (e.g. the R2 bucket front domain). Empty means the media package
	// default (https://m.friendfeed.me).
	MediaURL     string `json:"media_url"`
	GAuthKeyFile string `json:"gauth_key_file"`

	// Cloudflare R2 (S3 compatible) media bucket. Mirrored objects are written
	// to both MediaPath and this bucket. All four fields empty selects explicit
	// local-only mode; a partial configuration disables mirroring with an error.
	R2AccountID        string `json:"r2_account_id"`
	R2AccessKeyID      string `json:"r2_access_key_id"`
	R2SecretAccessKey  string `json:"r2_secret_access_key"`
	R2Bucket           string `json:"r2_bucket"`
	TwitterApiKey      string `json:"twitter_api_key"`
	TwitterApiSecret   string `json:"twitter_api_secret"`
	TwitterApiCallback string `json:"twitter_api_callback"`
}

func NewConfigFromJSON(filename string) (*Config, error) {
	rawdata, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	config := new(Config)
	if err := json.Unmarshal(rawdata, &config); err != nil {
		return nil, err
	}
	return config, nil
}
