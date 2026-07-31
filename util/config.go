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

	ServerDomain       string `json:"server_domain"`
	MediaPath          string `json:"media_path"`
	// MediaURL is the base URL mirrored media objects are served from
	// (e.g. the R2 bucket front domain). Empty means the media package
	// default (https://m.friendfeed.me).
	MediaURL           string `json:"media_url"`
	GAuthKeyFile       string `json:"gauth_key_file"`
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
