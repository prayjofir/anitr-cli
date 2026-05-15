package helpers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// ConfigDir platforma göre config dizinini döner.
// Linux/Mac: ~/.config/anitr-cli
// Windows:   %APPDATA%\anitr-cli
func ConfigDir() string {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			userProfile := os.Getenv("USERPROFILE")
			if userProfile == "" {
				if home, err := os.UserHomeDir(); err == nil {
					userProfile = home
				} else {
					userProfile = "."
				}
			}
			appData = filepath.Join(userProfile, "AppData", "Roaming")
		}
		primary := filepath.Join(appData, "anitr-cli")
		legacy := filepath.Join(appData, "AnitrCLI")
		if _, err := os.Stat(legacy); err == nil {
			if _, err2 := os.Stat(primary); err2 != nil {
				return legacy
			}
		}
		return primary
	}
	// Linux / macOS
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		if h := os.Getenv("HOME"); h != "" {
			home = h
		} else {
			home = "."
		}
	}
	return filepath.Join(home, ".config", "anitr-cli")
}

// IsValidImage, verilen URL'nin geçerli bir görsel olup olmadığını kontrol eder.
func IsValidImage(url string) bool {
	client := &http.Client{}
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	return resp.StatusCode == 200 && strings.HasPrefix(contentType, "image/")
}

func Slugify(s string) string {
	// Türkçe karakterleri İngilizce karşılıkları ile değiştir
	replacements := map[string]string{
		"Ü": "U",
		"İ": "I",
		"Ğ": "G",
		"Ş": "S",
		"ü": "u",
		"ı": "i",
		"ğ": "g",
		"ş": "s",
		"ö": "o",
		"Ö": "O",
		"ç": "c",
		"Ç": "C",
	}

	for k, v := range replacements {
		s = strings.ReplaceAll(s, k, v)
	}

	// Boşlukları - ile değiştir
	s = strings.ReplaceAll(s, " ", "-")

	// -, ", ' dışındaki tüm ASCII karakterleri sil
	re := regexp.MustCompile(`[^a-zA-Z0-9\-"']`)
	s = re.ReplaceAllString(s, "")

	return s
}

// Ptr, verilen değerin pointer'ını döner (herhangi bir tip için).
func Ptr[T any](val T) *T {
	return &val
}

// DefaultDownloadDir işletim sistemine göre varsayılan indirme dizinini döner
func DefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Fallback: ortam değişkenlerine bak
		if runtime.GOOS == "windows" {
			home = os.Getenv("USERPROFILE")
		} else {
			home = os.Getenv("HOME")
		}
		if home == "" {
			home = "."
		}
	}
	return filepath.Join(home, "Downloads", "anitr-cli")
}

var episodeRegex = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*\.?\s*Bölüm`)

func ExtractSeasonEpisode(title string) (episode float64, err error) {
	episode = 0

	// Bölümü ara
	if em := episodeRegex.FindStringSubmatch(title); len(em) >= 2 {
		episode, err = strconv.ParseFloat(em[1], 64)
		if err != nil {
			return 0, fmt.Errorf("bölüm parse edilemedi: %w", err)
		}
	} else {
		return 0, fmt.Errorf("bölüm numarası bulunamadı: %s", title)
	}

	return episode, nil
}
