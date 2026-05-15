package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/prayjofir/anitr-cli/internal/helpers"
)

// Config struct
type Config struct {
	LastSource        string `json:"last_source"`        // Son kullanılan kaynak (otomatik hatırlanır)
	HistoryLimit      int    `json:"history_limit"`
	DisableRPC        *bool  `json:"disable_rpc"`
	DownloadDir       string `json:"download_dir"`
	PreferredQuality  string `json:"preferred_quality"`  // "4K", "1080p", "720p", "480p"
	PreferredSubtitle string `json:"preferred_subtitle"` // "tr", "en", "de", "ar", "fr", "es", "it"
	PreferredSound    string `json:"preferred_sound"`    // "original", "trdub", "endub", "cndub"
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

// SaveConfig — config dosyasını günceller (mevcut içeriği koruyarak sadece verilen alanı yazar)
func SaveConfig(path string, updateFn func(*Config)) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		cfg = &Config{}
	}
	updateFn(cfg)
	if cfg.DownloadDir == "" {
		cfg.DownloadDir = helpers.DefaultDownloadDir()
	}
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
