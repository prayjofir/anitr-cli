package actions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axrona/anitr-cli/internal"
	"github.com/axrona/anitr-cli/internal/config"
	"github.com/axrona/anitr-cli/internal/dl"
	"github.com/axrona/anitr-cli/internal/helpers"
	"github.com/axrona/anitr-cli/internal/history"
	"github.com/axrona/anitr-cli/internal/models"
	"github.com/axrona/anitr-cli/internal/player"
	"github.com/axrona/anitr-cli/internal/sources/animecix"
	"github.com/axrona/anitr-cli/internal/sources/openanime"
	"github.com/axrona/anitr-cli/internal/ui"
	"github.com/axrona/anitr-cli/internal/ui/tui"
	"github.com/axrona/anitr-cli/internal/utils"
)

// Seçilen animeyi oynatma döngüsünü yönetir.
// Kullanıcıdan izleme seçenekleri alır, çözünürlük/fansub seçtirir, animeyi oynatır ve Discord RPC'yi günceller.
func PlayAnimeLoop(
	source models.AnimeSource, // Seçilen anime kaynağı (OpenAnime, AnimeciX)
	SelectedSource string, // Seçilen kaynak ismi
	episodes []models.Episode, // Tüm bölümler
	episodeNames []string, // Bölüm adları
	selectedAnimeID int, // Seçilen anime ID'si (AnimeciX için)
	selectedAnimeSlug string, // Seçilen anime slug'ı (OpenAnime için)
	selectedAnimeName string, // Seçilen anime ismi
	isMovie bool, // Film mi yoksa dizi mi olduğunu belirtir
	selectedSeasonIndex int, // Seçilen sezonun index'i
	UiMode string, // Arayüz tipi (örneğin terminal, rofi, vs.)
	RofiFlags string, // Rofi için özel bayraklar
	posterURL string, // Poster görseli URL'si (Discord RPC için)
	DisableRPC bool, // Discord RPC devre dışı mı?
	timestamp time.Time, // Discord RPC timestamp
	AnimeHistory models.AnimeHistory, // Geçmiş veri tipi
	Logger *models.LogServ, // Logger
) (models.AnimeSource, string, error) { // Geriye güncel kaynak ve kaynak ismi döner

	selectedEpisodeIndex := 0
	selectedFansubIdx := 0
	selectedResolution := ""
	selectedResolutionIdx := 0

	lastEpisodeIdxP := AnimeHistory[strings.ToLower(source.Source())][selectedAnimeName].LastEpisodeIdx

	lastEpisodeIdx := -1
	if lastEpisodeIdxP != nil {
		lastEpisodeIdx = *lastEpisodeIdxP
	}
	if lastEpisodeIdx >= 0 && len(episodes) > lastEpisodeIdx+1 {
		// Eğer daha önce izlenmişse bir sonraki bölüm
		selectedEpisodeIndex = lastEpisodeIdx + 1
	}

	for {
		ui.ClearScreen()

		// Kullanıcıya sunulacak menü seçenekleri
		watchMenu := []string{}
		if !isMovie {
			watchMenu = append(watchMenu, "İzle", "Sonraki bölüm", "Önceki bölüm", "Bölüm seç", "Çözünürlük seç", "Bölüm indir")
		} else {
			watchMenu = append(watchMenu, "İzle", "Çözünürlük seç", "Movie indir")
		}

		// OpenAnime için fansub seçimi
		if strings.ToLower(SelectedSource) == "openanime" {
			idx := -1
			for i, v := range watchMenu {
				if v == "Bölüm indir" || v == "Movie indir" {
					idx = i
					break
				}
			}

			if idx != -1 {
				watchMenu = append(watchMenu[:idx], append([]string{"Fansub seç"}, watchMenu[idx:]...)...)
			}
		}

		// Genel seçenekler
		watchMenu = append(watchMenu, "────────────────────", "Anime ara", "Çık")

		// Menü başlığını hazırla - bölüm bilgisi ile
		menuTitle := selectedAnimeName
		if !isMovie {
			currentEpisode := episodeNames[selectedEpisodeIndex]
			menuTitle = fmt.Sprintf("%s ( %s )", selectedAnimeName, currentEpisode)
		}

		// Seçim arayüzünü göster
		option, err := utils.ShowSelection(models.App{UiMode: &UiMode, RofiFlags: &RofiFlags}, watchMenu, menuTitle)

		if errors.Is(err, tui.ErrGoBack) {
			return nil, "", err
		}

		utils.FailIfErr(internal.UiParams{
			Mode:      UiMode,
			RofiFlags: &RofiFlags,
		}, err, Logger)

		switch option {

		// Oynatma ve bölüm gezme seçenekleri
		case "İzle", "Sonraki bölüm", "Önceki bölüm":
			ui.ClearScreen()

			if option == "Sonraki bölüm" {
				if selectedEpisodeIndex+1 >= len(episodes) {
					fmt.Println("Zaten son bölümdesiniz.")
					break
				}
				selectedEpisodeIndex++
			} else if option == "Önceki bölüm" {
				if selectedEpisodeIndex <= 0 {
					fmt.Println("Zaten ilk bölümdesiniz.")
					break
				}
				selectedEpisodeIndex--
			}

			// Loading spinner başlat
			done := make(chan struct{})
			go ui.ShowLoading(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, "Başlatılıyor...", done)

			// Güncel sezon bilgisi al
			selectedSeasonIndex = int(episodes[selectedEpisodeIndex].Extra["season_num"].(float64)) - 1

			// API'den oynatma bilgilerini güncelle
			data, _, err := utils.UpdateWatchAPI(
				strings.ToLower(SelectedSource),
				episodes,
				selectedEpisodeIndex,
				selectedAnimeID,
				selectedSeasonIndex,
				selectedFansubIdx,
				isMovie,
				&selectedAnimeSlug,
			)
			if err != nil {
				close(done)      // spinneri durdur
				ui.ClearScreen() // ekranı temizle
				fmt.Printf("\033[31m[!] Bölüm oynatılamadı: %s\033[0m\n", err)
				time.Sleep(1500 * time.Millisecond)
				continue
			}

			labels := data["labels"].([]string)
			urls := data["urls"].([]string)
			subtitle := data["caption_url"].(string)

			// Varsayılan çözünürlük seçimi
			if selectedResolution == "" {
				selectedResolutionIdx = 0
				if len(labels) > 0 {
					selectedResolution = labels[selectedResolutionIdx]
				}
			}
			if selectedResolutionIdx >= len(urls) {
				selectedResolutionIdx = len(urls) - 1
			}

			// MPV başlığı ayarla
			mpvTitle := fmt.Sprintf("%s - %s", selectedAnimeName, episodeNames[selectedEpisodeIndex])
			if isMovie {
				mpvTitle = selectedAnimeName
			}

			// MPV ile oynat
			cmd, socketPath, err := player.Play(player.MPVParams{
				Url:         urls[selectedResolutionIdx],
				SubtitleUrl: &subtitle,
				Title:       mpvTitle,
			})
			if !utils.CheckErr(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, err, Logger) {
				close(done) // spinneri durdur
				return source, SelectedSource, err
			}

			// MPV’nin çalışıp çalışmadığını kontrol et
			maxAttempts := 10
			mpvRunning := false
			for i := 0; i < maxAttempts; i++ {
				time.Sleep(300 * time.Millisecond)
				if player.IsMPVRunning(socketPath) {
					mpvRunning = true
					break
				}
			}
			if !mpvRunning {
				close(done)      // spinneri durdur
				ui.ClearScreen() // ekranı temizle
				err := fmt.Errorf("MPV başlatılamadı veya zamanında yanıt vermedi")
				utils.LogError(Logger, err)
				return source, SelectedSource, err
			}

			// Loading spinner durdur
			close(done)

			var stopCh chan struct{}
			if !DisableRPC {
				stopCh = make(chan struct{}) // Goroutine'i durdurmak için kanal oluştur
				go utils.UpdateDiscordRPC(socketPath, episodeNames, selectedEpisodeIndex, selectedAnimeName, SelectedSource, posterURL, timestamp, Logger, stopCh)
			}

			var selectedAnimeId string

			if strings.ToLower(source.Source()) == "animecix" {
				selectedAnimeId = strconv.Itoa(selectedAnimeID)
			} else {
				selectedAnimeId = selectedAnimeSlug
			}

			// History güncelleme için goroutine
			go history.UpdateAnimeHistory(socketPath, strings.ToLower(source.Source()), selectedAnimeName, episodeNames[selectedEpisodeIndex], selectedAnimeId, selectedEpisodeIndex, Logger)

			// Oynatma işlemi tamamlanana kadar bekle
			err = cmd.Wait()
			if err != nil {
				err = fmt.Errorf("MPV çalışırken hata: %w", err)
				utils.LogError(Logger, err)
				return source, SelectedSource, err
			}

			if stopCh != nil {
				// MPV kapandı → RPC goroutine'ini durdur
				close(stopCh)
			}

		// Çözünürlük seçme ekranı
		case "Çözünürlük seç":

			// Loading spinner başlat
			done := make(chan struct{})
			go ui.ShowLoading(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, "Hazırlanıyor...", done)

			data, _, err := utils.UpdateWatchAPI(
				strings.ToLower(SelectedSource),
				episodes,
				selectedEpisodeIndex,
				selectedAnimeID,
				selectedSeasonIndex,
				selectedFansubIdx,
				isMovie,
				&selectedAnimeSlug,
			)
			if err != nil {
				close(done)      // spinneri durdur
				ui.ClearScreen() // ekranı temizle

				fmt.Printf("\033[31m[!] Çözünürlükler yüklenemedi.\033[0m\n")
				time.Sleep(1000 * time.Millisecond)
				continue
			}
			labels := data["labels"].([]string)

			// Loading spinner durdur
			close(done)

			selected, err := utils.ShowSelection(models.App{UiMode: &UiMode, RofiFlags: &RofiFlags}, labels, "Çözünürlük seç ")

			if errors.Is(err, tui.ErrGoBack) {
				continue
			}

			if !utils.CheckErr(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, err, Logger) {
				continue
			}
			selectedResolution = selected
			if !slices.Contains(labels, selected) {
				fmt.Printf("\033[31m[!] Geçersiz çözünürlük seçimi: %s\033[0m\n", selected)
				time.Sleep(1500 * time.Millisecond)
				continue
			}
			selectedResolutionIdx = slices.Index(labels, selected)

		// Bölüm seçimi
		case "Bölüm seç":
			selected, err := utils.ShowSelection(models.App{UiMode: &UiMode, RofiFlags: &RofiFlags}, episodeNames, "Bölüm seç ")

			if errors.Is(err, tui.ErrGoBack) {
				continue
			}

			if !utils.CheckErr(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, err, Logger) {
				continue
			}
			if slices.Contains(episodeNames, selected) {
				selectedEpisodeIndex = slices.Index(episodeNames, selected)
				if !isMovie && selectedEpisodeIndex >= 0 && selectedEpisodeIndex < len(episodes) {
					selectedSeasonIndex = int(episodes[selectedEpisodeIndex].Extra["season_num"].(float64)) - 1
				}
			} else {
				continue
			}

		// Fansub seçimi (yalnızca OpenAnime için)
		case "Fansub seç":
			// Loading spinner başlat
			done := make(chan struct{})
			go ui.ShowLoading(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, "Hazırlanıyor...", done)

			fansubNames := []string{}

			if strings.ToLower(source.Source()) != "openanime" {
				close(done)      // spinneri durdur
				ui.ClearScreen() // ekranı temizle

				fmt.Println("\033[31m[!] Bu seçenek sadece OpenAnime için geçerlidir.\033[0m")
				time.Sleep(1500 * time.Millisecond)
				continue
			}

			_, fansubData, err := utils.UpdateWatchAPI(
				strings.ToLower(SelectedSource),
				episodes,
				selectedEpisodeIndex,
				selectedAnimeID,
				selectedSeasonIndex,
				selectedFansubIdx,
				isMovie,
				&selectedAnimeSlug,
			)
			if err != nil {
				close(done)      // spinneri durdur
				ui.ClearScreen() // ekranı temizle

				fmt.Printf("\033[31m[!] Fansublar yüklenemedi.\033[0m\n")
				time.Sleep(1000 * time.Millisecond)
				continue
			}

			for _, fansub := range fansubData {
				if fansub.Name != nil {
					fansubNames = append(fansubNames, *fansub.Name)
				}
			}

			// Loading spinner durdur
			close(done)

			selected, err := utils.ShowSelection(models.App{UiMode: &UiMode, RofiFlags: &RofiFlags}, fansubNames, "Fansub seç ")

			if errors.Is(err, tui.ErrGoBack) {
				continue
			}

			if !utils.CheckErr(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, err, Logger) {
				continue
			}

			if !slices.Contains(fansubNames, selected) {
				fmt.Printf("\033[31m[!] Geçersiz fansub seçimi: %s\033[0m\n", selected)
				time.Sleep(1500 * time.Millisecond)
				continue
			}
			selectedFansubIdx = slices.Index(fansubNames, selected)

		// Movie / Bölüm indir
		case "Bölüm indir", "Movie indir":
			ui.ClearScreen()

			cfg, err := config.LoadConfig(filepath.Join(utils.ConfigDir(), "config.json"))
			if err != nil {
				cfg = &config.Config{} // eğer config yoksa varsayılan config oluştur
			}

			if cfg.DownloadDir == "" {
				defaultDir := helpers.DefaultDownloadDir()
				fmt.Printf("Videoları nereye indirmek istersiniz? (Varsayılan: %s): ", defaultDir)
				var input string
				fmt.Scanln(&input)
				if input == "" {
					input = defaultDir
				}
				cfg.DownloadDir = input

				// Config dosyasına kaydet
				os.MkdirAll(utils.ConfigDir(), 0o755)
				f, err := os.Create(filepath.Join(utils.ConfigDir(), "config.json"))
				if err == nil {
					defer f.Close()
					enc := json.NewEncoder(f)
					enc.SetIndent("", "  ")
					enc.Encode(cfg)
				}
			}

			// Downloader için cfg.DownloadDir kullan
			downloader, err := dl.NewDownloader(cfg.DownloadDir)
			if err != nil {
				switch {
				case errors.Is(err, dl.ErrNoDownloader):
					fmt.Printf("\033[31m[!] yt-dlp veya youtube-dl bulunamadı\033[0m\n")
				case errors.Is(err, dl.ErrDirCreate):
					fmt.Printf("\033[31m[!] Klasör oluşturulamadı: %v\033[0m\n", err)
				default:
					fmt.Printf("\033[31m[!] Hata: %v\033[0m\n", err)
				}
				time.Sleep(1500 * time.Millisecond)
				continue
			}

			var choices []string

			if option == "Bölüm indir" {
				choices, err = ui.MultiSelectList(internal.UiParams{
					Mode:      UiMode,
					List:      &episodeNames,
					RofiFlags: &RofiFlags,
					Label:     "Bölüm seç ",
				})

				if errors.Is(err, tui.ErrGoBack) {
					continue
				}

				if err != nil {
					fmt.Printf("\033[31m[!] Seçim listesi oluşturulamadı: %s\033[0m\n", err)
					time.Sleep(1500 * time.Millisecond)
					continue
				}
			} else {
				// Movie ise zaten tek bölüm
				choices = []string{episodeNames[0]}
			}

			// Seçilen bölümleri filtrele
			selectedEpisodes := make([]models.Episode, 0, len(choices))
			episodeNameSet := make(map[string]struct{}, len(choices))

			for _, c := range choices {
				episodeNameSet[c] = struct{}{}
			}

			for _, ep := range episodes {
				if _, ok := episodeNameSet[ep.Title]; ok {
					selectedEpisodes = append(selectedEpisodes, ep)
				}
			}

			// Güncel sezon bilgisi
			if len(selectedEpisodes) > 0 {
				selectedSeasonIndex = int(selectedEpisodes[0].Extra["season_num"].(float64)) - 1
			}

			// Loading spinner başlat
			done := make(chan struct{})
			go ui.ShowLoading(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, "İndiriliyor...", done)

			// Seçilen çözünürlüğe göre tüm bölümlerin URL'lerini al
			links, err := utils.GetSelectedEpidodesLinks(
				strings.ToLower(SelectedSource),
				selectedEpisodes,
				selectedFansubIdx,
				isMovie,
				&selectedAnimeSlug,
				selectedResolution,
				selectedAnimeID,
			)
			if err != nil {
				close(done)      // spinneri durdur
				ui.ClearScreen() // ekranı temizle

				fmt.Printf("\033[31m[!] Bölüm URL'leri alınamadı: %s\033[0m\n", err)
				time.Sleep(1500 * time.Millisecond)
				continue
			}

			// Loading spinner durdur
			close(done)
			// Yazıyı temizle
			ui.ClearScreen()

			// Downloader ile indirme işlemi
			for _, ep := range selectedEpisodes {
				url, ok := links[ep.Title]
				if !ok {
					fmt.Printf("\033[31m[!] %s için URL bulunamadı.\033[0m\n", ep.Title)
					continue
				}

				episodeNumber, err := helpers.ExtractSeasonEpisode(ep.Title)
				if err != nil {
					fmt.Printf("\033[31m[!] %s için bölüm numarası çıkarılamadı: %s\033[0m\n", ep.Title, err)
					continue
				}

				seasonNumber, ok := ep.Extra["season_num"].(float64)
				if !ok {
					utils.LogError(Logger, fmt.Errorf("season_num float64 değil"))
				}

				err = downloader.Download(strings.ToLower(source.Source()), selectedAnimeName, url, episodeNumber, int(seasonNumber))
				if err != nil {
					fmt.Printf("\033[31m[!] %s indirilemedi: %s\033[0m\n", ep.Title, err)
				}
			}

		// Yeni bir anime aramak için menü
		case "Anime ara":
			for {
				choice, err := utils.ShowSelection(models.App{UiMode: &UiMode, RofiFlags: &RofiFlags}, []string{"Bu kaynakla devam et", "Kaynak değiştir", "Çık"}, fmt.Sprintf("Arama kaynağı: %s", SelectedSource))

				if errors.Is(err, tui.ErrGoBack) {
					break
				}

				if err != nil {
					utils.LogError(Logger, fmt.Errorf("seçim listesi oluşturulamadı: %w", err))
					continue
				}

				switch choice {
				case "Bu kaynakla devam et":
					// Hiçbir işlem yapma
				case "Kaynak değiştir":
					SelectedSource, source = utils.SelectSource(UiMode, RofiFlags, source, Logger)
				case "Çık":
					os.Exit(0)
				default:
					fmt.Printf("\033[31m[!] Geçersiz seçim: %s\033[0m\n", choice)
					time.Sleep(1500 * time.Millisecond)
					continue
				}

				return source, SelectedSource, nil
			}

		// Çıkış seçeneği
		case "Çık":
			os.Exit(0)

		default:
			return source, SelectedSource, nil
		}
	}
}

// En son izlenen animeyi hızlıca devam ettiren fonksiyon
func QuickResumeLastAnime(cfx *models.App, timestamp time.Time) error {
	// Geçmişi kontrol et
	if cfx.AnimeHistory == nil || len(*cfx.AnimeHistory) == 0 {
		return fmt.Errorf("geçmiş bulunamadı")
	}

	// En son izlenen animeyi bul
	var latestAnime string
	var latestAnimeId string
	var latestEpisodeIdx int
	var latestSource string
	var latestTime time.Time

	for sourceName, sourceData := range *cfx.AnimeHistory {
		for animeName, entry := range sourceData {
			if entry.LastWatched != nil && entry.LastWatched.After(latestTime) {
				latestTime = *entry.LastWatched
				latestAnime = animeName
				latestAnimeId = *entry.AnimeId
				latestEpisodeIdx = *entry.LastEpisodeIdx
				latestSource = sourceName
			}
		}
	}

	if latestAnime == "" {
		return fmt.Errorf("geçmişte anime bulunamadı")
	}

	// Kaynağı ayarla
	var source models.AnimeSource
	switch strings.ToLower(latestSource) {
	case "openanime":
		source = openanime.OpenAnime{}
		cfx.SelectedSource = helpers.Ptr("OpenAnime")
	case "animecix":
		source = animecix.AnimeCix{}
		cfx.SelectedSource = helpers.Ptr("AnimeciX")
	default:
		return fmt.Errorf("geçersiz kaynak: %s", latestSource)
	}
	cfx.Source = &source

	fmt.Printf(" Son izlenen anime devam ettiriliyor: %s\n", latestAnime)

	// Anime bilgilerini al
	animeData, err := source.GetAnimeByID(latestAnimeId)
	if err != nil {
		return fmt.Errorf("anime bilgileri alınamadı: %w", err)
	}

	// Anime ID ve slug'ını al
	selectedAnimeID, selectedAnimeSlug := utils.GetAnimeIDs(source, *animeData)

	// Poster URL'si
	posterURL := animeData.ImageURL
	if !helpers.IsValidImage(posterURL) {
		posterURL = "anitrcli"
	}

	// Bölümleri al
	episodes, episodeNames, isMovie, selectedSeasonIndex, err := utils.GetEpisodesAndNames(
		source, false, selectedAnimeID, selectedAnimeSlug, animeData.Title,
	)
	if err != nil {
		return fmt.Errorf("bölümler alınamadı: %w", err)
	}

	// Son bölümden devam et
	if latestEpisodeIdx >= len(episodes) {
		latestEpisodeIdx = len(episodes) - 1
	}

	// Oynatma döngüsüne gir
	_, _, err = PlayAnimeLoop(
		source, *cfx.SelectedSource, episodes, episodeNames,
		selectedAnimeID, selectedAnimeSlug, animeData.Title,
		isMovie, selectedSeasonIndex, *cfx.UiMode, *cfx.RofiFlags,
		posterURL, *cfx.DisableRPC, timestamp, *cfx.AnimeHistory, cfx.Logger,
	)

	return err
}
