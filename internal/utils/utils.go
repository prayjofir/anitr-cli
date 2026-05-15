package utils

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prayjofir/anitr-cli/internal"
	"github.com/prayjofir/anitr-cli/internal/helpers"
	"github.com/prayjofir/anitr-cli/internal/models"
	"github.com/prayjofir/anitr-cli/internal/player"
	"github.com/prayjofir/anitr-cli/internal/rpc"
	"github.com/prayjofir/anitr-cli/internal/sources/animecix"
	"github.com/prayjofir/anitr-cli/internal/sources/anizium"
	"github.com/prayjofir/anitr-cli/internal/sources/aniziumfree"
	"github.com/prayjofir/anitr-cli/internal/sources/openanime"
	"github.com/prayjofir/anitr-cli/internal/ui"
	"github.com/prayjofir/anitr-cli/internal/ui/tui"
)

var ErrQuit = errors.New("quit requested")

// getTempDir işletim sistemine göre geçici dizin döner.
func getTempDir() string {
	if runtime.GOOS == "windows" {
		// Windows ortam değişkeni TEMP veya TMP
		if temp := os.Getenv("TEMP"); temp != "" {
			return temp
		}
		if tmp := os.Getenv("TMP"); tmp != "" {
			return tmp
		}
		// Fallback
		return `C:\Temp`
	}
	// Unix benzeri sistemler için /tmp
	return "/tmp"
}

// ConfigDir platforma göre config dizinini döner.
// helpers.ConfigDir() üzerinden çağrılır — tek kaynak, tutarlı davranış.
func ConfigDir() string {
	return helpers.ConfigDir()
}

// OpenPath, verilen dosya veya dizini OS'in varsayılan uygulamasıyla açar
func OpenPath(path string) error {
    if path == "" {
        return fmt.Errorf("boş yol açılamaz")
    }
    // Yol mevcut mu kontrol et (dosya veya dizin)
    if _, err := os.Stat(path); err != nil {
        return fmt.Errorf("yol mevcut değil: %w", err)
    }

    switch runtime.GOOS {
    case "windows":
        // Powershell/Windows'ta 'rundll32 url.dll,FileProtocolHandler <path>' veya 'start' kullanılabilir.
        // exec.Command ile doğrudan 'rundll32' güvenilir çalışır.
        cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
        return cmd.Start()
    case "darwin":
        cmd := exec.Command("open", path)
        return cmd.Start()
    default:
        // linux
        cmd := exec.Command("xdg-open", path)
        return cmd.Start()
    }
}

// NewLogger, işletim sistemine göre uygun dizinde bir log dosyası oluşturur ve Logger döner.
func NewLogger() (*models.LogServ, error) {
	tempDir := getTempDir()
	logPath := filepath.Join(tempDir, "anitr-cli.log")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("log dosyası açılamadı: %w", err)
	}

	Logger := log.New(file, "", log.LstdFlags|log.Lmsgprefix)

	return &models.LogServ{
		File: file,
		Log:  Logger,
	}, nil
}

// LogError, hata objesini loglar (nil ise işlem yapılmaz).
func LogError(l *models.LogServ, err error) {
	if err == nil {
		return
	}

	l.Log.Printf("[ERROR] %v\n", err)
}

// LogMsg, belirtilen formatta bir log mesajı yazar.
func LogMsg(l *models.LogServ, format string, a ...interface{}) {
	l.Log.Printf(format, a...)
}

// Close, log dosyasını kapatır.
func Close(l *models.LogServ) error {
	return l.File.Close()
}

// FailIfErr, kritik hatalarda loglar, kullanıcıyı bilgilendirir ve uygulamayı kapatır.
func FailIfErr(params internal.UiParams, err error, Logger *models.LogServ) {
	if err != nil {
		if errors.Is(err, ErrQuit) {
			os.Exit(0)
		}

		LogError(Logger, err)
		ui.ShowError(params, fmt.Sprint(err))
		Close(Logger)
		os.Exit(1)
	}
}

// CheckErr, hata varsa loglar, ekranda gösterir ve kullanıcıdan devam için giriş bekler.
func CheckErr(params internal.UiParams, err error, Logger *models.LogServ) bool {
	if err != nil {
		if errors.Is(err, ErrQuit) {
			os.Exit(0)
		}

		LogError(Logger, err)
		ui.ShowError(params, fmt.Sprint(err))
		fmt.Scanln()
		return false
	}
	return true
}

// Seçilen animeye ait bölümleri getirir, isim listesi oluşturur ve movie olup olmadığını döner
func GetEpisodesAndNames(source models.AnimeSource, isMovie bool, selectedAnimeID int, selectedAnimeSlug string, selectedAnimeName string) ([]models.Episode, []string, bool, int, error) {
	var (
		episodes            []models.Episode
		episodeNames        []string
		selectedSeasonIndex int
		err                 error
	)

	// OpenAnime ise sezon verisini alarak movie olup olmadığını kontrol et
	if strings.ToLower(source.Source()) == "openanime" {
		seasonData, err := source.GetSeasonsData(models.SeasonParams{Slug: &selectedAnimeSlug})
		if err != nil {
			return nil, nil, false, 0, fmt.Errorf("sezon verisi alınamadı: %w", err)
		}
		isMovie = *seasonData[0].IsMovie
	}

	if !isMovie {
		// Dizi ise bölüm verilerini al
		episodes, err = source.GetEpisodesData(models.EpisodeParams{SeasonID: &selectedAnimeID, Slug: &selectedAnimeSlug})
		if err != nil {
			return nil, nil, false, 0, fmt.Errorf("bölüm verisi alınamadı: %w", err)
		}

		if len(episodes) == 0 {
			return nil, nil, false, 0, fmt.Errorf("hiçbir bölüm bulunamadı")
		}

		// Bölüm isimlerini listeye ekle
		episodeNames = make([]string, 0, len(episodes))
		for _, e := range episodes {
			episodeNames = append(episodeNames, e.Title)
		}

		// Sezon indeksini belirle
		selectedSeasonIndex = int(episodes[0].Extra["season_num"].(float64)) - 1
	} else {
		// Film ise sadece tek bir bölüm olarak ayarla
		episodeNames = []string{selectedAnimeName}
		episodes = []models.Episode{{
			Title: selectedAnimeName,
			Extra: map[string]interface{}{"season_num": float64(1)},
		}}
		selectedSeasonIndex = 0
	}

	return episodes, episodeNames, isMovie, selectedSeasonIndex, nil
}

// UpdateWatchAPI, seçilen kaynağa (animecix veya openanime) göre bir bölümün izlenebilir URL'lerini ve altyazı bilgilerini getirir.
// Ayrıca varsa TR altyazı URL'sini de döner.
// Params:
// - source: kaynak adı ("animecix", "openanime")
// - episodeData: bölüm listesi
// - index: seçilen bölümün dizindeki yeri
// - id: anime ID'si
// - seasonIndex: sezonun sıfırdan başlayan indeksi
// - selectedFansubIndex: openanime için seçilen fansub'un sırası
// - isMovie: film mi dizi mi
// - slug: openanime için gerekli olan tanımlayıcı
//
// Returns:
// - İzlenebilir kaynakları ve altyazı URL'sini içeren map[string]interface{}
// - Eğer openanime seçildiyse, fansub'ları içeren []models.Fansub
// - Hata (varsa)
func UpdateWatchAPI(
	source string,
	episodeData []models.Episode,
	index, id, seasonIndex, selectedFansubIndex int,
	isMovie bool,
	slug *string,
) (map[string]interface{}, []models.Fansub, error) {
	var (
		captionData []map[string]string // Video etiketleri ve URL'leri
		fansubData  []models.Fansub     // Fansub listesi (openanime için)
		captionURL  string              // Türkçe altyazı URL'si
		err         error
	)

	switch source {
	case "animecix":
		// Film ise farklı API kullan
		if isMovie {
			data, err := animecix.AnimeMovieWatchApiUrl(id)
			if err != nil {
				return nil, nil, fmt.Errorf("animecix movie API çağrısı başarısız: %w", err)
			}
			// Caption URL ve video stream'leri al
			captionURLIface := data["caption_url"]
			captionURL, _ = captionURLIface.(string)
			streamsIface, ok := data["video_streams"]
			if !ok {
				return nil, nil, fmt.Errorf("video_streams beklenen formatta değil")
			}
			rawStreams, _ := streamsIface.([]interface{})
			for _, streamIface := range rawStreams {
				stream, _ := streamIface.(map[string]interface{})
				label := internal.GetString(stream, "label")
				url := internal.GetString(stream, "url")
				captionData = append(captionData, map[string]string{"label": label, "url": url})
			}
		} else {
			// Dizi bölümü için
			if index < 0 || index >= len(episodeData) {
				return nil, nil, fmt.Errorf("index out of range")
			}
			urlData := episodeData[index].ID
			captionData, err = animecix.AnimeWatchApiUrl(urlData)
			if err != nil {
				return nil, nil, fmt.Errorf("animecix watch API çağrısı başarısız: %w", err)
			}
			// Sezon içerisindeki bölüm indeksini bul
			seasonEpisodeIndex := 0
			for i := 0; i < index; i++ {
				if sn, ok := episodeData[i].Extra["season_num"].(int); ok {
					if sn-1 == seasonIndex {
						seasonEpisodeIndex++
					}
				} else if snf, ok := episodeData[i].Extra["season_num"].(float64); ok {
					if int(snf)-1 == seasonIndex {
						seasonEpisodeIndex++
					}
				}
			}
			// TR altyazı URL'sini almaya çalış
			captionURL, err = animecix.FetchTRCaption(seasonIndex, seasonEpisodeIndex, id)
			if err != nil {
				captionURL = ""
			}
		}

	case "anizium", "anizium free":
		if index < 0 || index >= len(episodeData) {
			return nil, nil, fmt.Errorf("index out of range")
		}

		watchParams := models.WatchParams{
			Slug:    slug,
			Id:      &id,
			IsMovie: &isMovie,
			Url:     &episodeData[index].ID,
			Extra: &map[string]interface{}{
				"seasonIndex":           seasonIndex,
				"episodeIndex":          index,
				"skip_sound_preference": false, // indirme flow'u bunu true yapar
			},
		}

		var watches []models.Watch
		if source == "anizium free" {
			watches, err = aniziumfree.AniziumFree{}.GetWatchData(watchParams)
		} else {
			watches, err = anizium.Anizium{}.GetWatchData(watchParams)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("watch API çağrısı başarısız: %w", err)
		}

		if len(watches) > 0 {
			w := watches[0]
			for i := range w.Labels {
				captionData = append(captionData, map[string]string{
					"label": w.Labels[i],
					"url":   w.Urls[i],
				})
			}
			if w.TRCaption != nil {
				captionURL = *w.TRCaption
			}
			// Tüm altyazı seçeneklerini map'e ekle
			if len(w.Subtitles) > 0 {
				return map[string]interface{}{
					"labels":       extractLabels(captionData),
					"urls":         extractURLs(captionData),
					"caption_url":  captionURL,
					"subtitles":    w.Subtitles,
					"warn_message": w.WarnMessage,
				}, nil, nil
			}
			// Altyazı yoksa, warn_message'ı yine de ilet
			if w.WarnMessage != "" {
				return map[string]interface{}{
					"labels":       extractLabels(captionData),
					"urls":         extractURLs(captionData),
					"caption_url":  captionURL,
					"warn_message": w.WarnMessage,
				}, nil, nil
			}
		}

	case "openanime":
		if slug == nil {
			return nil, nil, fmt.Errorf("slug gerekli")
		}
		if index < 0 || index >= len(episodeData) {
			return nil, nil, fmt.Errorf("index out of range")
		}
		ep := episodeData[index]
		seasonNum := 0
		episodeNum := 0

		// Sezon ve bölüm numaralarını al
		if sn, ok := ep.Extra["season_num"].(int); ok {
			seasonNum = sn
		} else if snf, ok := ep.Extra["season_num"].(float64); ok {
			seasonNum = int(snf)
		} else {
			return nil, nil, fmt.Errorf("season_num beklenen formatta değil")
		}
		if en, ok := ep.Extra["episode_num"].(int); ok {
			episodeNum = en
		} else if enf, ok := ep.Extra["episode_num"].(float64); ok {
			episodeNum = int(enf)
		} else {
			episodeNum = ep.Number
		}

		// Fansub listesini al
		fansubParams := models.FansubParams{
			Slug:       slug,
			SeasonNum:  &seasonNum,
			EpisodeNum: &episodeNum,
		}
		fansubData, err = openanime.OpenAnime{}.GetFansubsData(fansubParams)
		if err != nil {
			return nil, nil, fmt.Errorf("fansub data API çağrısı başarısız: %w", err)
		}
		if selectedFansubIndex < 0 || selectedFansubIndex >= len(fansubData) {
			return nil, nil, fmt.Errorf("seçilen fansub indeksi geçersiz")
		}

		// İzlenebilir veri isteği yap
		watchParams := models.WatchParams{
			Slug:    slug,
			Id:      &id,
			IsMovie: &isMovie,
			Extra: &map[string]interface{}{
				"season_num":         seasonNum,
				"episode_num":        episodeNum,
				"fansubs":            fansubData,
				"selected_fansub_id": selectedFansubIndex,
			},
		}
		watches, err := openanime.OpenAnime{}.GetWatchData(watchParams)
		if err != nil {
			return nil, nil, fmt.Errorf("openanime watch data alınamadı: %w", err)
		}
		if len(watches) < 1 {
			return nil, nil, fmt.Errorf("openanime watch data boş")
		}
		w := watches[0]
		captionData = make([]map[string]string, len(w.Labels))
		for i := range w.Labels {
			captionData[i] = map[string]string{
				"label": w.Labels[i],
				"url":   w.Urls[i],
			}
		}
		if w.TRCaption != nil {
			captionURL = *w.TRCaption
		}

	default:
		return nil, nil, fmt.Errorf("geçersiz kaynak: %s", source)
	}

	// Kaliteye göre (etiket sayısal değerine göre) sırala
	sort.Slice(captionData, func(i, j int) bool {
		labelI := strings.TrimRight(captionData[i]["label"], "p")
		labelJ := strings.TrimRight(captionData[j]["label"], "p")
		intI, _ := strconv.Atoi(labelI)
		intJ, _ := strconv.Atoi(labelJ)
		return intI > intJ
	})

	// Etiketleri ve URL'leri ayır
	labels := []string{}
	urls := []string{}
	for _, item := range captionData {
		labels = append(labels, item["label"])
		urls = append(urls, item["url"])
	}

	return map[string]interface{}{
		"labels":      labels,
		"urls":        urls,
		"caption_url": captionURL,
	}, fansubData, nil
}

func extractLabels(captionData []map[string]string) []string {
	out := make([]string, 0, len(captionData))
	for _, item := range captionData {
		out = append(out, item["label"])
	}
	return out
}

func extractURLs(captionData []map[string]string) []string {
	out := make([]string, 0, len(captionData))
	for _, item := range captionData {
		out = append(out, item["url"])
	}
	return out
}

// GetSelectedEpisodesLinks, seçilen bölümlerin sadece seçilmiş çözünürlük URL'lerini döner
func GetSelectedEpidodesLinks(
	source string,
	episodes []models.Episode,
	selectedFansubIndex int,
	isMovie bool,
	slug *string,
	selectedResolution string, // kullanıcı seçimi: "4K (Japonca)", "480p (Türkçe Dublaj)" vb.
	selectedAnimeID int,
) (map[string]string, error) {
	result := make(map[string]string)

	for _, ep := range episodes {
		var labels []string
		var urls []string

		if source == "anizium" || source == "anizium free" {
			// İndirme modunda tüm kalite+ses etiketlerini al (tercih filtresi devre dışı)
			seasonIdx := 0
			if snRaw, ok := ep.Extra["season_num"]; ok {
				if snf, ok2 := snRaw.(float64); ok2 {
					seasonIdx = int(snf) - 1
				}
			}
			wpExtra := map[string]interface{}{
				"seasonIndex":           seasonIdx,
				"episodeIndex":          0,
				"skip_sound_preference": true,
			}
			var watches []models.Watch
			var getWatchErr error
			wp := models.WatchParams{
				Id:      &selectedAnimeID,
				IsMovie: &isMovie,
				Url:     &ep.ID,
				Extra:   &wpExtra,
			}
			if source == "anizium free" {
				watches, getWatchErr = aniziumfree.AniziumFree{}.GetWatchData(wp)
			} else {
				watches, getWatchErr = anizium.Anizium{}.GetWatchData(wp)
			}
			if getWatchErr != nil {
				return nil, fmt.Errorf("[%s] anizium watch hatası: %w", ep.Title, getWatchErr)
			}
			if len(watches) > 0 {
				labels = watches[0].Labels
				urls = watches[0].Urls
			}
		} else {
			// Diğer kaynaklar
			data, _, err := UpdateWatchAPI(
				source,
				[]models.Episode{ep},
				0,
				selectedAnimeID,
				0,
				selectedFansubIndex,
				isMovie,
				slug,
			)
			if err != nil {
				return nil, fmt.Errorf("[%s] updateWatchAPI hatası: %w", ep.Title, err)
			}
			if lbls, ok := data["labels"].([]string); ok {
				labels = lbls
			}
			if us, ok := data["urls"].([]string); ok {
				urls = us
			}
		}

		if len(urls) == 0 {
			return nil, fmt.Errorf("[%s] url bulunamadı", ep.Title)
		}

		// Seçilen etiketle eşleşen URL'yi bul
		resolutionIdx := 0
		for i, label := range labels {
			if label == selectedResolution {
				resolutionIdx = i
				break
			}
		}
		if resolutionIdx >= len(urls) {
			resolutionIdx = len(urls) - 1
		}

		result[ep.Title] = urls[resolutionIdx]
	}

	return result, nil
}

// Seçilen animenin ID veya slug bilgisini döner
func GetAnimeIDs(source models.AnimeSource, selectedAnime models.Anime) (int, string) {
	var selectedAnimeID int
	var selectedAnimeSlug string

	// Kaynağa göre ID veya slug alınır
	if strings.ToLower(source.Source()) == "animecix" || strings.ToLower(source.Source()) == "anizium" || strings.ToLower(source.Source()) == "anizium free" {
		if selectedAnime.ID != nil {
			selectedAnimeID = *selectedAnime.ID
		}
	} else if strings.ToLower(source.Source()) == "openanime" {
		if selectedAnime.Slug != nil {
			selectedAnimeSlug = *selectedAnime.Slug
		}
	}
	return selectedAnimeID, selectedAnimeSlug
}

// Kullanıcıdan kaynak seçmesini isteyen fonksiyon
func SelectSource(UiMode, RofiFlags string, defaultSource models.AnimeSource, Logger *models.LogServ) (string, models.AnimeSource) {
	for {
		// Kaynak listesi (OpenAnime şimdilik devre dışı)
		sourceList := []string{"AnimeciX", "Anizium", "Anizium Free"}

		// Kullanıcıdan seçim al
		SelectedSource, err := ShowSelection(
			models.App{UiMode: &UiMode, RofiFlags: &RofiFlags},
			sourceList,
			"Kaynak seç",
		)

		if errors.Is(err, tui.ErrGoBack) {
			// direkt eski menüye dön
			return defaultSource.Source(), defaultSource
		}

		if err != nil {
			// Kullanıcı iptal ettiyse default'a dön veya menüye at
			LogError(Logger, err)
			return "", nil
		}

		// Normalize et
		src := strings.ToLower(strings.TrimSpace(SelectedSource))

		// Kaynağı eşleştir
		switch src {
		// case "openanime": // OpenAnime şimdilik devre dışı
		// 	return SelectedSource, openanime.OpenAnime{}
		case "animecix":
			return SelectedSource, animecix.AnimeCix{}
		case "anizium":
			return SelectedSource, anizium.Anizium{}
		case "anizium free":
			return SelectedSource, aniziumfree.AniziumFree{}
		default:
			fmt.Printf("\033[31m[!] Geçersiz kaynak seçimi: %s\033[0m\n", SelectedSource)
			time.Sleep(1500 * time.Millisecond)
			continue
		}
	}
}

// Kullanıcıdan bir seçim almak için kullanılan fonksiyon
func ShowSelection(cfx models.App, list []string, label string) (string, error) {
	return ui.SelectionList(internal.UiParams{
		Mode:      *cfx.UiMode,
		RofiFlags: cfx.RofiFlags,
		List:      &list,
		Label:     label,
	})
}

// GetSeasonPlaylistData, seçili sezonun tüm bölümleri için URL'leri ve altyazıları toplar
func GetSeasonPlaylistData(
	source string,
	episodes []models.Episode,
	selectedFansubIdx int,
	isMovie bool,
	slug *string,
	selectedResolution string,
	selectedAnimeID int,
	episodeNames []string,
	selectedAnimeName string,
	Logger *models.LogServ,
	progressCallback func(current, total int, episodeName string), // Progress callback
) ([]player.MPVParams, error) {
	var playlistParams []player.MPVParams
	var failedEpisodes []string

	for i, ep := range episodes {
		// Progress callback çağır
		if progressCallback != nil {
			progressCallback(i+1, len(episodes), ep.Title)
		}

		// Progress gösterimi için
		if Logger != nil {
			LogMsg(Logger, "Playlist hazırlanıyor: %d/%d - %s", i+1, len(episodes), ep.Title)
		}

		// Tek bölüm için veri al
		data, _, err := UpdateWatchAPI(
			source,
			[]models.Episode{ep}, // tek bölüm
			0,                    // index 0 çünkü slice sadece 1 eleman
			selectedAnimeID,
			0, // sezon index kullanılacaksa güncellenebilir
			selectedFansubIdx,
			isMovie,
			slug,
		)
		if err != nil {
			failedEpisodes = append(failedEpisodes, ep.Title)
			continue
		}

		labelsIface, ok := data["labels"].([]string)
		urlsIface, ok2 := data["urls"].([]string)
		captionURLIface, ok3 := data["caption_url"].(string)
		if !ok || !ok2 {
			failedEpisodes = append(failedEpisodes, ep.Title)
			continue
		}
		labels := labelsIface
		urls := urlsIface
		captionURL := captionURLIface

		// Seçilen çözünürlük için index bul
		resolutionIdx := 0
		for j, label := range labels {
			if label == selectedResolution {
				resolutionIdx = j
				break
			}
		}
		if resolutionIdx >= len(urls) {
			resolutionIdx = len(urls) - 1
		}

		// MPV parametrelerini hazırla
		mpvTitle := fmt.Sprintf("%s - %s", selectedAnimeName, ep.Title)
		if isMovie {
			mpvTitle = selectedAnimeName
		}

		var subtitleUrl *string
		if ok3 && captionURL != "" {
			subtitleUrl = &captionURL
		}

		playlistParams = append(playlistParams, player.MPVParams{
			Url:         urls[resolutionIdx],
			SubtitleUrl: subtitleUrl,
			Title:       mpvTitle,
		})
	}

	// Başarısız bölümleri logla
	if len(failedEpisodes) > 0 && Logger != nil {
		LogMsg(Logger, "Playlist hazırlanırken %d bölüm başarısız oldu: %v", len(failedEpisodes), failedEpisodes)
	}

	if len(playlistParams) == 0 {
		return nil, fmt.Errorf("hiçbir bölüm için URL alınamadı")
	}

	return playlistParams, nil
}

// Discord RPC'yi güncelleyerek anime oynatma durumunu Discord'a yansıtır
func UpdateDiscordRPC(socketPath string, episodeNames []string, selectedEpisodeIndex int,
	selectedAnimeName, SelectedSource, posterURL string, timestamp time.Time, Logger *models.LogServ, stopCh <-chan struct{},
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			// Stop sinyali geldi → Discord RPC'yi kapat
			rpc.ClientLogout()
			return
		case <-ticker.C:
			// Eğer MPV çalışmıyorsa da RPC'yi kapat ve çık
			if !player.IsMPVRunning(socketPath) {
				rpc.ClientLogout()
				return
			}

			// Playlist modunda çalışıyor mu kontrol et
			playlistCount, err := player.GetPlaylistCount(socketPath)
			isPlaylistMode := err == nil && playlistCount > 1

			var currentEpisodeIndex int
			var currentEpisodeName string

			if isPlaylistMode {
				// Playlist modunda mevcut pozisyonu al
				currentPos, err := player.GetCurrentPlaylistPos(socketPath)
				if err != nil || currentPos < 0 || currentPos >= len(episodeNames) {
					continue
				}
				currentEpisodeIndex = currentPos
				currentEpisodeName = episodeNames[currentPos]
			} else {
				// Normal modda seçili bölümü kullan
				currentEpisodeIndex = selectedEpisodeIndex
				currentEpisodeName = episodeNames[selectedEpisodeIndex]
			}

			// MPV duraklatma durumu
			isPaused, _ := player.GetMPVPausedStatus(socketPath)

			// MPV süre ve konum
			durationVal, _ := player.MPVSendCommand(socketPath, []interface{}{"get_property", "duration"})
			timePosVal, _ := player.MPVSendCommand(socketPath, []interface{}{"get_property", "time-pos"})
			duration, ok1 := durationVal.(float64)
			timePos, ok2 := timePosVal.(float64)
			if !ok1 || !ok2 {
				continue
			}

			formatTime := func(seconds float64) string {
				total := int(seconds + 0.5)
				hours := total / 3600
				minutes := (total % 3600) / 60
				secs := total % 60
				if hours > 0 {
					return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, secs)
				}
				return fmt.Sprintf("%02d:%02d", minutes, secs)
			}

			// State'i oluştur
			var state string
			if isPlaylistMode {
				state = fmt.Sprintf("%s (%d/%d) (%s / %s)", 
					currentEpisodeName, 
					currentEpisodeIndex+1, 
					playlistCount, 
					formatTime(timePos), 
					formatTime(duration))
			} else {
				state = fmt.Sprintf("%s (%s / %s)", 
					currentEpisodeName, 
					formatTime(timePos), 
					formatTime(duration))
			}
			
			if isPaused {
				state += " (Paused)"
			}

			params := internal.RPCParams{
				Type:       3,
				Details:    selectedAnimeName,
				State:      state,
				SmallImage: strings.ToLower(SelectedSource),
				SmallText:  SelectedSource,
				LargeImage: posterURL,
				LargeText:  selectedAnimeName,
				Timestamp:  timestamp,
			}

			if err := rpc.DiscordRPC(params); err != nil {
				LogError(Logger, fmt.Errorf("DiscordRPC hatası: %w", err))
				continue
			}
		}
	}
}
