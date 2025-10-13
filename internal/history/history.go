package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/axrona/anitr-cli/internal/models"
	"github.com/axrona/anitr-cli/internal/player"
	"github.com/axrona/anitr-cli/internal/utils"
)

// getHistoryPath cross-platform olarak history.json yolunu döndürür
func getHistoryPath() (string, error) {
	// ConfigDir() ile aynı yeri kullanarak platformlar arasında tutarlılık sağlar.
	historyDir := utils.ConfigDir()

	// Klasör yoksa oluştur
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		return "", fmt.Errorf("history klasörü oluşturulamadı: %w", err)
	}

	return filepath.Join(historyDir, "history.json"), nil
}

// GetHistoryPath history.json dosyasının tam yolunu döner
// Klasör mevcut değilse oluşturur.
func GetHistoryPath() (string, error) {
    return getHistoryPath()
}

// ReadAnimeHistory history.json'u okur, yoksa yeni oluşturur
func ReadAnimeHistory() (models.AnimeHistory, error) {
	path, err := getHistoryPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(models.AnimeHistory), nil
		}
		return nil, fmt.Errorf("history okunamadı: %w", err)
	}

	var history models.AnimeHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("history parse edilemedi: %w", err)
	}
	return history, nil
}

// WriteAnimeHistory history.json'u yazar
func WriteAnimeHistory(history models.AnimeHistory) error {
	path, err := getHistoryPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("history serialize edilemedi: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("history yazılamadı: %w", err)
	}
	return nil
}

// UpdateAnimeHistory, mevcut MPV oturumu sırasında animeyi history.json'a kaydeder
func UpdateAnimeHistory(socketPath, source, animeName, episodeName, animeId string, episodeIndex int, logserv *models.LogServ) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	updated := false
	for range ticker.C {
		if !player.IsMPVRunning(socketPath) {
			break
		}

		durationVal, err1 := player.MPVSendCommand(socketPath, []interface{}{"get_property", "duration"})
		timePosVal, err2 := player.MPVSendCommand(socketPath, []interface{}{"get_property", "time-pos"})
		if err1 != nil || err2 != nil {
			continue
		}

		duration, ok1 := durationVal.(float64)
		progress, ok2 := timePosVal.(float64)
		if !ok1 || !ok2 {
			continue
		}

		if updated {
			continue
		}

		if progress >= duration-300 { // son 5 dakika
			history, err := ReadAnimeHistory()
			if err != nil {
				utils.LogError(logserv, err)
				continue
			}

			sourceEntry, ok := history[source]
			if !ok {
				sourceEntry = make(map[string]models.AnimeHistoryEntry)
			}

			animeEntry, ok := sourceEntry[animeName]
			if !ok {
				animeEntry = models.AnimeHistoryEntry{}
			}

			time := time.Now()

			animeEntry = models.AnimeHistoryEntry{
				LastEpisodeIdx:  &episodeIndex,
				LastEpisodeName: episodeName,
				AnimeId:         &animeId,
				LastWatched:     &time,
			}
			sourceEntry[animeName] = animeEntry
			history[source] = sourceEntry

			if err := WriteAnimeHistory(history); err != nil {
				utils.LogError(logserv, err)
				continue
			}
			updated = true
		}
	}
}
