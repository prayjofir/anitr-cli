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
	historyDir := utils.ConfigDir()
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		return "", fmt.Errorf("history klasörü oluşturulamadı: %w", err)
	}
	return filepath.Join(historyDir, "history.json"), nil
}

// GetHistoryPath history.json dosyasının tam yolunu döner
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

// DeleteAnimeHistory — belirtilen animeni geçmişten siler
func DeleteAnimeHistory(source, animeName string) error {
	hist, err := ReadAnimeHistory()
	if err != nil {
		return err
	}
	if sourceData, ok := hist[source]; ok {
		delete(sourceData, animeName)
		hist[source] = sourceData
	}
	return WriteAnimeHistory(hist)
}

// UpdateAnimeHistory — MPV oturumu boyunca pozisyonu ve tamamlanma durumunu kaydeder.
//
//   - Her 10 saniyede bir `time-pos` okunur ve diske yazılır.
//   - İzlenme %90'ı geçtiyse bölüm "bitti" (IsFinished=true) olarak işaretlenir.
//   - totalEpisodeCount: o anki toplam bölüm sayısı (yeni bölüm tespiti için).
func UpdateAnimeHistory(
	socketPath, source, animeName, episodeName, animeId string,
	episodeIndex, totalEpisodeCount int,
	isMovie bool,
	logserv *models.LogServ,
) {
	saveHistory := func(positionSec *float64, isFinished bool) {
		hist, err := ReadAnimeHistory()
		if err != nil {
			utils.LogError(logserv, err)
			return
		}
		sourceEntry, ok := hist[source]
		if !ok {
			sourceEntry = make(map[string]models.AnimeHistoryEntry)
		}
		now := time.Now()
		sourceEntry[animeName] = models.AnimeHistoryEntry{
			LastEpisodeIdx:    &episodeIndex,
			LastEpisodeName:   episodeName,
			AnimeId:           &animeId,
			LastWatched:       &now,
			LastPositionSec:   positionSec,
			IsFinished:        isFinished,
			TotalEpisodeCount: totalEpisodeCount,
			IsMovie:           isMovie,
		}
		hist[source] = sourceEntry
		if err := WriteAnimeHistory(hist); err != nil {
			utils.LogError(logserv, err)
		}
	}

	// İlk kayıt — MPV başlar başlamaz
	saveHistory(nil, false)

	ticker := time.NewTicker(5 * time.Second) // 5s'de bir güncelle (10s yerine)
	defer ticker.Stop()

	var lastKnownPos *float64
	lastKnownFinished := false

	for range ticker.C {
		if !player.IsMPVRunning(socketPath) {
			break
		}

		timePosVal, err1 := player.MPVSendCommand(socketPath, []interface{}{"get_property", "time-pos"})
		durationVal, err2 := player.MPVSendCommand(socketPath, []interface{}{"get_property", "duration"})
		if err1 != nil || err2 != nil {
			continue
		}

		timePos, ok1 := timePosVal.(float64)
		duration, ok2 := durationVal.(float64)
		if !ok1 || !ok2 || duration <= 0 {
			continue
		}

		progress := timePos / duration
		if progress >= 0.9 {
			// Bölüm tamamlandı — pozisyonu sıfırla, bitti olarak işaretle
			saveHistory(nil, true)
			lastKnownFinished = true
			break
		} else {
			// Devam ediyor — mevcut pozisyonu kaydet
			pos := timePos
			lastKnownPos = &pos
			saveHistory(lastKnownPos, false)
		}
	}

	// MPV kapandı — henüz bitmemişse son bilinen pozisyonu kaydet
	if !lastKnownFinished {
		saveHistory(lastKnownPos, false)
	}
}
