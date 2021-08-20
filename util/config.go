package util

import (
	"encoding/json"
	"io/ioutil"
	"log"
)

// ----------------------------
// Server Config
// ----------------------------
type Config struct {
	Address    string `json:"address"`
	DBPath     string `json:"db_path"`
	Debug      bool   `json:"debug"`
	GcsAppId   string `json:"gcs_app_id"`
	GcsBucket  string `json:"gcs_bucket"`
	GcsKeyFile string `json:"gcs_key_file"`

	MediaPath          string `json:"media_path"`
	GAuthKeyFile       string `json:"gauth_key_file"`
	TwitterApiKey      string `json:"twitter_api_key"`
	TwitterApiSecret   string `json:"twitter_api_secret"`
	TwitterApiCallback string `json:"twitter_api_callback"`
}

func NewConfigFromJSON(filename string) (*Config, error) {
	rawdata, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}

	config := new(Config)
	if err := json.Unmarshal(rawdata, &config); err != nil {
		return nil, err
	}
	return config, nil
}
