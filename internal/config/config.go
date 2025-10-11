package config

import (
	"encoding/json"
	"os"

	"github.com/axrona/anitr-cli/internal/helpers"
)

// Config struct
type Config struct {
	DefaultSource string `json:"default_source"`
	HistoryLimit  int    `json:"history_limit"`
	DisableRPC    *bool  `json:"disable_rpc"`
	DownloadDir   string `json:"download_dir"`
}

// LoadConfig config'i yükler
func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, err
	}

	if cfg.DownloadDir == "" {
		cfg.DownloadDir = helpers.DefaultDownloadDir()
	}

	return &cfg, nil
}
