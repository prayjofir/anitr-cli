package actions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
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
	"github.com/axrona/anitr-cli/internal/sources/anizium"
	"github.com/axrona/anitr-cli/internal/sources/aniziumfree"
	"github.com/axrona/anitr-cli/internal/sources/openanime"
	"github.com/axrona/anitr-cli/internal/ui"
	"github.com/axrona/anitr-cli/internal/ui/tui"
	"github.com/axrona/anitr-cli/internal/utils"
)

// Seçilen animeyi oynatma döngüsünü yönetir.

// ErrEpisodeNotFinished — bölüm bitmeden çıkıldı; geçmiş listesine dönülmesi gerektiğini bildirir.
var ErrEpisodeNotFinished = errors.New("episode not finished")

// configPath returns the path to anitr-cli's config.json
func configPath() string {
	return filepath.Join(utils.ConfigDir(), "config.json")
}

// subtitleHumanLabel altyazı dil kodunu okunabilir isme çevirir.
func subtitleHumanLabel(lang string) string {
	switch strings.ToLower(lang) {
	case "tr":
		return "Türkçe"
	case "en":
		return "İngilizce"
	case "de":
		return "Almanca"
	case "ar":
		return "Arapça"
	case "fr":
		return "Fransızca"
	case "es":
		return "İspanyolca"
	case "it":
		return "İtalyanca"
	default:
		return lang
	}
}

// buildSeasonDisplay, bölüm listesine sezon ayırıcıları ekler.
// Dönen displayList TUI'ya verilir; nameToIdx seçimi orijinal indekse çevirir.
func buildSeasonDisplay(episodes []models.Episode, names []string) (displayList []string, nameToIdx map[string]int) {
	nameToIdx = make(map[string]int)

	// Birden fazla sezon var mı kontrol et
	hasMulti := false
	for _, ep := range episodes {
		if snRaw, ok := ep.Extra["season_num"]; ok {
			if snf, ok2 := snRaw.(float64); ok2 && int(snf) > 1 {
				hasMulti = true
				break
			}
		}
	}

	if !hasMulti {
		// Tek sezon — ayraç ekleme, direkt döndür
		for i, name := range names {
			displayList = append(displayList, name)
			nameToIdx[name] = i
		}
		return
	}

	currentSeason := -1
	for i, ep := range episodes {
		sn := 1
		if snRaw, ok := ep.Extra["season_num"]; ok {
			if snf, ok2 := snRaw.(float64); ok2 {
				sn = int(snf)
			}
		}
		if sn != currentSeason {
			currentSeason = sn
			// TUI bu formatı seasonSeparatorItem olarak tanır
			separator := fmt.Sprintf("────────── %d. Sezon ──────────", sn)
			displayList = append(displayList, separator)
		}
		name := names[i]
		displayList = append(displayList, name)
		nameToIdx[name] = i
	}
	return
}

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
	autoPlay bool, // Geçmişten gelince direkt oynat (menü göstermeden)
) (models.AnimeSource, string, error) { // Geriye güncel kaynak ve kaynak ismi döner

	selectedEpisodeIndex := 0
	selectedFansubIdx := 0
	selectedResolution := ""
	selectedResolutionIdx := 0
	autoPlayTriggered := false
	var nextEpisodeFromAPI *models.NextEpisodeData // Anizium'dan gelen sonraki bölüm bilgisi

	histEntry := AnimeHistory[strings.ToLower(source.Source())][selectedAnimeName]
	lastEpisodeIdxP := histEntry.LastEpisodeIdx

	lastEpisodeIdx := -1
	if lastEpisodeIdxP != nil {
		lastEpisodeIdx = *lastEpisodeIdxP
	}
	if lastEpisodeIdx >= 0 {
		if histEntry.IsFinished && len(episodes) > lastEpisodeIdx+1 {
			// Bölüm bitti — sonrakine geç
			selectedEpisodeIndex = lastEpisodeIdx + 1
		} else {
			// Bitmemiş veya son bölüm — aynı bölümde kal
			selectedEpisodeIndex = lastEpisodeIdx
		}
	}

	for {
		ui.ClearScreen()

		// autoPlay modunda ilk girişte direkt oynat (menü göstermeden)
		var option string
		var err error

		if autoPlay && !autoPlayTriggered {
			autoPlayTriggered = true
			option = "İzle"
		} else {
			// Kullanıcıya sunulacak menü seçenekleri
			watchMenu := []string{}
			if !isMovie {
				watchMenu = append(watchMenu, "İzle", "Sonraki bölüm", "Önceki bölüm", "Bölüm seç", "Tüm sezonu izle", "────────────────────", "Çözünürlük seç", "Bölüm indir")
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
			if history.IsFavorite(fmt.Sprintf("%d", selectedAnimeID), strings.ToLower(SelectedSource)) {
				watchMenu = append(watchMenu, "⭐ Favorilerden Çıkar", "────────────────────", "Anime ara", "Çık")
			} else {
				watchMenu = append(watchMenu, "⭐ Favorilere Ekle", "────────────────────", "Anime ara", "Çık")
			}

			// Menü başlığını hazırla - bölüm bilgisi ile
			menuTitle := selectedAnimeName
			if !isMovie {
				currentEpisode := episodeNames[selectedEpisodeIndex]
				menuTitle = fmt.Sprintf("%s ( %s )", selectedAnimeName, currentEpisode)
			}

			// Seçim arayüzünü göster
			option, err = utils.ShowSelection(models.App{UiMode: &UiMode, RofiFlags: &RofiFlags}, watchMenu, menuTitle)

			if errors.Is(err, tui.ErrGoBack) {
				return nil, "", err
			}

			utils.FailIfErr(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, err, Logger)
		}

		switch option {

		// Favori ekle / çıkar
		case "⭐ Favorilere Ekle":
			animeIDStr := fmt.Sprintf("%d", selectedAnimeID)
			history.AddFavorite(selectedAnimeName, animeIDStr, strings.ToLower(SelectedSource), isMovie)
			ui.ClearScreen()
			fmt.Printf("\033[32m⭐ '%s' favorilere eklendi!\033[0m\n", selectedAnimeName)
			time.Sleep(1200 * time.Millisecond)

		case "⭐ Favorilerden Çıkar":
			animeIDStr := fmt.Sprintf("%d", selectedAnimeID)
			history.RemoveFavorite(animeIDStr, strings.ToLower(SelectedSource))
			ui.ClearScreen()
			fmt.Printf("\033[33m⭐ '%s' favorilerden çıkarıldı.\033[0m\n", selectedAnimeName)
			time.Sleep(1200 * time.Millisecond)

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
			// "Sor" modunda (PreferredQuality=="") Anizium için tüm kaliteleri al
			var data map[string]interface{}
			if strings.ToLower(SelectedSource) == "anizium" || strings.ToLower(SelectedSource) == "anizium free" {
				preferredQuality := ""
				if appCfg, cfgErr := config.LoadConfig(configPath()); cfgErr == nil {
					preferredQuality = appCfg.PreferredQuality
				}
				skipPref := preferredQuality == "" // Sor modu → tüm kaliteleri getir
				wpExtra := map[string]interface{}{
					"seasonIndex":           selectedSeasonIndex,
					"episodeIndex":          selectedEpisodeIndex,
					"skip_sound_preference": skipPref,
				}
				var watches []models.Watch
				var wErr error
				if strings.ToLower(SelectedSource) == "anizium free" {
					watches, wErr = aniziumfree.AniziumFree{}.GetWatchData(models.WatchParams{
						Id: &selectedAnimeID, IsMovie: &isMovie,
						Url: &episodes[selectedEpisodeIndex].ID, Extra: &wpExtra,
					})
				} else {
					watches, wErr = anizium.Anizium{}.GetWatchData(models.WatchParams{
						Id: &selectedAnimeID, IsMovie: &isMovie,
						Url: &episodes[selectedEpisodeIndex].ID, Extra: &wpExtra,
					})
				}
				if wErr != nil || len(watches) == 0 {
					close(done)
					ui.ClearScreen()
					fmt.Printf("\033[31m[!] Bölüm oynatılamadı:\033[0m\n%v\n", wErr)
					fmt.Print("\nDevam etmek için ENTER'a basın...")
					fmt.Scanln()
					continue
				}
				w := watches[0]
				nextEpisodeFromAPI = w.NextEpisode
				data = map[string]interface{}{
					"labels": w.Labels,
					"urls":   w.Urls,
					"caption_url": func() string {
						if w.TRCaption != nil {
							return *w.TRCaption
						}
						return ""
					}(),
					"subtitles":    w.Subtitles,
					"warn_message": w.WarnMessage,
					"opening":      w.Opening, // intro bar için
					"ending":       w.Ending,  // outro bar için
				}
			} else {
				var err error
				data, _, err = utils.UpdateWatchAPI(
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
					close(done)
					ui.ClearScreen()
					fmt.Printf("\033[31m[!] Bölüm oynatılamadı:\033[0m\n%s\n", err)
					fmt.Print("\nDevam etmek için ENTER'a basın...")
					fmt.Scanln()
					continue
				}
			}

			labels := data["labels"].([]string)
			urls := data["urls"].([]string)
			subtitle := data["caption_url"].(string)

			// Ses fallback uyarısı (anizium.go'dan gelir)
			if warnMsg, ok := data["warn_message"].(string); ok && warnMsg != "" {
				close(done)
				fmt.Printf("\033[33m%s\033[0m\n", warnMsg)
				time.Sleep(1500 * time.Millisecond)
				done = make(chan struct{})
				go ui.ShowLoading(internal.UiParams{Mode: UiMode, RofiFlags: &RofiFlags}, "Başlatılıyor...", done)
			}

			// Anizium için çoklu altyazı: tüm URL'leri MPV'ye geç (indirme yok, anlık)
			var subtitleUrls []string
			if strings.ToLower(SelectedSource) == "anizium" || strings.ToLower(SelectedSource) == "anizium free" {
				if subsRaw, ok := data["subtitles"]; ok {
					if subs, ok := subsRaw.([]models.WatchSubtitle); ok && len(subs) > 0 {

						// Tercih edilen dili config'den oku
						preferredLang := ""
						if appCfg, cfgErr := config.LoadConfig(configPath()); cfgErr == nil {
							preferredLang = appCfg.PreferredSubtitle
						}

						// Altyazıları sırala: tercih → tr → en → geri kalan
						seen := map[string]bool{}
						var ordered []models.WatchSubtitle

						// Önce tercih edilen dil
						if preferredLang != "" {
							for _, s := range subs {
								if s.Group == preferredLang && !seen[s.Group] {
									seen[s.Group] = true
									ordered = append(ordered, s)
								}
							}
							if len(ordered) == 0 {
								fmt.Printf("\033[33m⚠️  Tercih edilen altyazı (%s) bulunamadı.\033[0m\n", subtitleHumanLabel(preferredLang))
								time.Sleep(800 * time.Millisecond)
							}
						}
						// Sonra TR (zaten eklenmemişse)
						for _, s := range subs {
							if s.Group == "tr" && !seen[s.Group] {
								seen[s.Group] = true
								ordered = append(ordered, s)
							}
						}
						// Geri kalanlar
						for _, s := range subs {
							if !seen[s.Group] {
								seen[s.Group] = true
								ordered = append(ordered, s)
							}
						}

						for _, s := range ordered {
							if s.Link != "" {
								subtitleUrls = append(subtitleUrls, s.Link)
							}
						}
					}
				}
			}

			// Varsayılan çözünürlük/ses seçimi
			if selectedResolution == "" {
				if strings.ToLower(SelectedSource) == "anizium" && len(labels) > 0 {
					// "Sor" modu (PreferredQuality=="") veya birden fazla ses seçeneği varsa sor
					preferredQuality := ""
					if appCfg, cfgErr := config.LoadConfig(configPath()); cfgErr == nil {
						preferredQuality = appCfg.PreferredQuality
					}

					shouldAsk := preferredQuality == "" || len(labels) > 1
					if shouldAsk {
						close(done) // spinner'ı durdur, seçim menüsü açılıyor

						selected, selErr := utils.ShowSelection(models.App{UiMode: &UiMode, RofiFlags: &RofiFlags}, labels, "Ses/Kalite seç ")
						if errors.Is(selErr, tui.ErrGoBack) {
							continue
						}
						if selErr == nil && slices.Contains(labels, selected) {
							selectedResolution = selected
							selectedResolutionIdx = slices.Index(labels, selected)
						} else {
							selectedResolutionIdx = 0
							selectedResolution = labels[0]
						}

						// Spinner'ı yeniden başlat (MPV açılıyor)
						done = make(chan struct{})
						go ui.ShowLoading(internal.UiParams{
							Mode:      UiMode,
							RofiFlags: &RofiFlags,
						}, "Başlatılıyor...", done)
					} else {
						selectedResolutionIdx = 0
						selectedResolution = labels[0]
					}
				} else {
					selectedResolutionIdx = 0
					if len(labels) > 0 {
						selectedResolution = labels[selectedResolutionIdx]
					}
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

			// Devam izleme: geçmişten kalindığı saniyeyi oku
			var startPositionSec float64
			if strings.ToLower(SelectedSource) == "anizium" || strings.ToLower(SelectedSource) == "anizium free" {
				if freshHist, hErr := history.ReadAnimeHistory(); hErr == nil {
					entry := freshHist[strings.ToLower(source.Source())][selectedAnimeName]
					// Sadece bu bölüm için ve bitmemişse pozisyonu kullan
					if entry.LastEpisodeIdx != nil &&
						*entry.LastEpisodeIdx == selectedEpisodeIndex &&
						!entry.IsFinished &&
						entry.LastPositionSec != nil &&
						*entry.LastPositionSec > 10 {
						startPositionSec = *entry.LastPositionSec
					}
				}
			}

			// Opening + Ending verisi — Lua script ile chapter inject + [A] skip
			var luaScript string
			var openingOp *models.OpeningData
			var endingOp *models.OpeningData
			if openingRaw, ok := data["opening"]; ok {
				if op, ok := openingRaw.(*models.OpeningData); ok && op != nil {
					openingOp = op
				}
			}
			if endingRaw, ok := data["ending"]; ok {
				if ed, ok := endingRaw.(*models.OpeningData); ok && ed != nil {
					endingOp = ed
				}
			}
			if openingOp != nil {
				luaScript = writeChapterLuaScript(openingOp, endingOp)
			}

			// MPV ile oynat
			mpvParams := player.MPVParams{
				Url:              urls[selectedResolutionIdx],
				Title:            mpvTitle,
				SubtitleUrls:     subtitleUrls,
				StartPositionSec: startPositionSec,
				LuaScript:        luaScript,
			}
			// Diğer kaynaklar için tek altyazı (subtitle değişkeni)
			if len(subtitleUrls) == 0 && subtitle != "" {
				mpvParams.SubtitleUrl = &subtitle
			}
			cmd, socketPath, err := player.Play(mpvParams)
			if !utils.CheckErr(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, err, Logger) {
				close(done) // spinneri durdur
				return source, SelectedSource, err
			}

			// MPV'nin çalışıp çalışmadığını kontrol et
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

			// Opening chapter'larını IPC ile set et (video yüklendikten sonra)
			if openingOp != nil {
				go func(op *models.OpeningData, sock string) {
					time.Sleep(1500 * time.Millisecond) // video yüklensin
					startSec := timeStringToSeconds(op.Start)
					endSec := timeStringToSeconds(op.End)
					chapterList := []map[string]interface{}{
						{"title": "Giriş", "time": float64(0)},
						{"title": "Opening", "time": startSec},
						{"title": "Bölüm", "time": endSec},
					}
					player.MPVSendCommand(sock, []interface{}{"set_property", "chapter-list", chapterList})
				}(openingOp, socketPath)
			}

			// Loading spinner durdur
			close(done)

			var stopCh chan struct{}
			if !DisableRPC {
				stopCh = make(chan struct{}) // Goroutine'i durdurmak için kanal oluştur
				go utils.UpdateDiscordRPC(socketPath, episodeNames, selectedEpisodeIndex, selectedAnimeName, SelectedSource, posterURL, timestamp, Logger, stopCh)
			}

			var selectedAnimeId string
			if strings.ToLower(source.Source()) == "animecix" || strings.ToLower(source.Source()) == "anizium" || strings.ToLower(source.Source()) == "anizium free" {
				selectedAnimeId = strconv.Itoa(selectedAnimeID)
			} else {
				selectedAnimeId = selectedAnimeSlug // openanime slug kullanır
			}

			// History güncelleme için goroutine
			go history.UpdateAnimeHistory(
				socketPath,
				strings.ToLower(source.Source()),
				selectedAnimeName,
				episodeNames[selectedEpisodeIndex],
				selectedAnimeId,
				selectedEpisodeIndex,
				len(episodes),
				isMovie,
				Logger,
			)

			// Oynatma işlemi tamamlanana kadar bekle
			err = cmd.Wait()
			if err != nil {
				err = fmt.Errorf("MPV çalışırken hata: %w", err)
				utils.LogError(Logger, err)
				return source, SelectedSource, err
			}

			if stopCh != nil {
				close(stopCh)
			}

			// Geçici Lua script dosyasını temizle
			if luaScript != "" {
				os.Remove(luaScript)
			}

			// History goroutine'inin son kaydını yapması için kısaca bekle
			time.Sleep(400 * time.Millisecond)

			// Bölüm bitti mi kontrol et (history'den oku)
			episodeFinished := false
			if freshHist, hErr := history.ReadAnimeHistory(); hErr == nil {
				entry := freshHist[strings.ToLower(source.Source())][selectedAnimeName]
				if entry.LastEpisodeIdx != nil &&
					*entry.LastEpisodeIdx == selectedEpisodeIndex &&
					entry.IsFinished {
					episodeFinished = true
				}
			}

			// Bitmeden çıkıldıysa — geçmiş listesine dön sinyali
			if !episodeFinished {
				return source, SelectedSource, ErrEpisodeNotFinished
			}

			// Anizium: next_episode_data ile sonraki bölüme geç
			if (strings.ToLower(SelectedSource) == "anizium" || strings.ToLower(SelectedSource) == "anizium free") && nextEpisodeFromAPI != nil {
				nxt := nextEpisodeFromAPI
				nextEpisodeFromAPI = nil

				// 1) Lokal listede ara
				newIdx := -1
				for i, ep := range episodes {
					epSeason := 1
					if snRaw, ok2 := ep.Extra["season_num"]; ok2 {
						if snf, ok3 := snRaw.(float64); ok3 {
							epSeason = int(snf)
						}
					}
					if epSeason == nxt.Season && ep.Number == nxt.Episode {
						newIdx = i
						break
					}
				}

				if newIdx >= 0 {
					// Zaten listede var
					selectedEpisodeIndex = newIdx
				} else {
					// Listede yok → yeni bölüm düşmüş olabilir, API'den taze çek
					freshEps, fetchErr := source.GetEpisodesData(models.EpisodeParams{SeasonID: &selectedAnimeID})
					if fetchErr == nil && len(freshEps) > 0 {
						if len(freshEps) > len(episodes) {
							ui.ClearScreen()
							fmt.Printf("\033[32m✨ Yeni bölüm eklendi! Bölüm listesi güncellendi.\033[0m\n")
							time.Sleep(1500 * time.Millisecond)
						}
						// Listeyi güncelle
						episodes = freshEps
						var freshNames []string
						for _, ep := range freshEps {
							freshNames = append(freshNames, ep.Title)
						}
						episodeNames = freshNames
						// Güncel listede ara
						for i, ep := range episodes {
							epSeason := 1
							if snRaw, ok2 := ep.Extra["season_num"]; ok2 {
								if snf, ok3 := snRaw.(float64); ok3 {
									epSeason = int(snf)
								}
							}
							if epSeason == nxt.Season && ep.Number == nxt.Episode {
								selectedEpisodeIndex = i
								break
							}
						}
					}
				}
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
			displayList, nameToIdx := buildSeasonDisplay(episodes, episodeNames)
			selected, err := ui.SelectionList(internal.UiParams{
				Mode:              UiMode,
				RofiFlags:         &RofiFlags,
				List:              &displayList,
				Label:             "Bölüm seç ",
				SkipAllSeparators: true, // ayraçlar seçilemesin
			})

			if errors.Is(err, tui.ErrGoBack) {
				continue
			}

			if !utils.CheckErr(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, err, Logger) {
				continue
			}
			if idx, ok := nameToIdx[selected]; ok {
				selectedEpisodeIndex = idx
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

		// Tüm sezonu playlist olarak izle
		case "Tüm sezonu izle":
			ui.ClearScreen()

			// Sezonları topla ve kullanıcıya seçtir
			seasonMap := make(map[int][]int) // season -> episode indices
			for i, ep := range episodes {
				seasonNum := int(ep.Extra["season_num"].(float64))
				seasonMap[seasonNum] = append(seasonMap[seasonNum], i)
			}

			// Sezon listesi oluştur
			var seasonNumbers []int
			for seasonNum := range seasonMap {
				seasonNumbers = append(seasonNumbers, seasonNum)
			}
			sort.Ints(seasonNumbers)

			// Sezon seçenekleri
			var seasonOptions []string
			for _, seasonNum := range seasonNumbers {
				episodeCount := len(seasonMap[seasonNum])
				seasonOptions = append(seasonOptions, fmt.Sprintf("Sezon %d (%d bölüm)", seasonNum, episodeCount))
			}

			// Kullanıcıdan sezon seçimi al
			selectedSeasonOption, err := utils.ShowSelection(models.App{UiMode: &UiMode, RofiFlags: &RofiFlags}, seasonOptions, "Sezon seç")
			if errors.Is(err, tui.ErrGoBack) {
				continue
			}
			if !utils.CheckErr(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, err, Logger) {
				continue
			}

			// Seçilen sezonu bul
			selectedSeasonIdx := -1
			for i, option := range seasonOptions {
				if option == selectedSeasonOption {
					selectedSeasonIdx = i
					break
				}
			}
			if selectedSeasonIdx == -1 {
				continue
			}

			selectedSeasonNum := seasonNumbers[selectedSeasonIdx]
			seasonEpisodeIndices := seasonMap[selectedSeasonNum]

			// Seçili sezonun bölümlerini filtrele
			var seasonEpisodes []models.Episode
			var seasonEpisodeNames []string
			for _, idx := range seasonEpisodeIndices {
				seasonEpisodes = append(seasonEpisodes, episodes[idx])
				seasonEpisodeNames = append(seasonEpisodeNames, episodeNames[idx])
			}

			if len(seasonEpisodes) == 0 {
				ui.ClearScreen()
				fmt.Printf("\033[31m[!] Bu sezon için bölüm bulunamadı.\033[0m\n")
				time.Sleep(1500 * time.Millisecond)
				continue
			}

			// Loading spinner başlat
			done := make(chan struct{})
			go ui.ShowLoading(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, fmt.Sprintf("Sezon %d playlist hazırlanıyor... (%d bölüm)", selectedSeasonNum, len(seasonEpisodes)), done)

			// Varsayılan çözünürlük seçimi
			if selectedResolution == "" {
				// İlk bölümden çözünürlük al
				data, _, err := utils.UpdateWatchAPI(
					strings.ToLower(SelectedSource),
					[]models.Episode{seasonEpisodes[0]},
					0,
					selectedAnimeID,
					selectedSeasonIndex,
					selectedFansubIdx,
					isMovie,
					&selectedAnimeSlug,
				)
				if err != nil {
					close(done)
					ui.ClearScreen()
					fmt.Printf("\033[31m[!] Çözünürlük bilgisi alınamadı: %s\033[0m\n", err)
					time.Sleep(1500 * time.Millisecond)
					continue
				}
				labels := data["labels"].([]string)
				if len(labels) > 0 {
					selectedResolution = labels[0]
				}
			}

			// Progress callback fonksiyonu
			progressCallback := func(current, total int, episodeName string) {
				// Loading mesajını güncelle
				ui.UpdateLoadingMessage(internal.UiParams{
					Mode:      UiMode,
					RofiFlags: &RofiFlags,
				}, fmt.Sprintf("Sezon %d playlist hazırlanıyor... (%d/%d) %s", selectedSeasonNum, current, total, episodeName))
			}

			// Playlist verilerini topla
			playlistParams, err := utils.GetSeasonPlaylistData(
				strings.ToLower(SelectedSource),
				seasonEpisodes,
				selectedFansubIdx,
				isMovie,
				&selectedAnimeSlug,
				selectedResolution,
				selectedAnimeID,
				seasonEpisodeNames,
				selectedAnimeName,
				Logger,
				progressCallback,
			)
			if err != nil {
				close(done)
				ui.ClearScreen()
				fmt.Printf("\033[31m[!] Playlist hazırlanamadı: %s\033[0m\n", err)
				time.Sleep(1500 * time.Millisecond)
				continue
			}

			// Loading spinner durdur
			close(done)

			// History'den başlangıç pozisyonunu al
			startIndex := 0
			var selectedAnimeId string
			if strings.ToLower(source.Source()) == "animecix" || strings.ToLower(source.Source()) == "anizium" || strings.ToLower(source.Source()) == "anizium free" {
				selectedAnimeId = strconv.Itoa(selectedAnimeID)
			} else {
				selectedAnimeId = selectedAnimeSlug // openanime slug kullanır
			}
			
			// History'den son izlenen bölümü kontrol et
			if lastEpisodeIdx >= 0 {
				// Son izlenen bölüm bu sezonun içinde mi?
				for i, globalIdx := range seasonEpisodeIndices {
					if globalIdx == lastEpisodeIdx {
						startIndex = i
						break
					}
				}
			}

			// MPV ile playlist'i başlat (startIndex ile)
			cmd, socketPath, err := player.PlayWithPlaylist(playlistParams, startIndex)
			if !utils.CheckErr(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, err, Logger) {
				return source, SelectedSource, err
			}

			// MPV'nin çalışıp çalışmadığını kontrol et
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
				ui.ClearScreen()
				err := fmt.Errorf("MPV başlatılamadı veya zamanında yanıt vermedi")
				utils.LogError(Logger, err)
				return source, SelectedSource, err
			}

			// İlk history kaydını hemen yap (MPV başladıktan sonra)
			initialGlobalIndex := startIndex
			if startIndex >= 0 && startIndex < len(seasonEpisodeIndices) {
				initialGlobalIndex = seasonEpisodeIndices[startIndex]
			}
			initialEpisodeName := seasonEpisodeNames[startIndex]
			
			// İlk bölüm için history'yi güncelle
			hist, err := history.ReadAnimeHistory()
			if err == nil {
				sourceEntry, ok := hist[strings.ToLower(source.Source())]
				if !ok {
					sourceEntry = make(map[string]models.AnimeHistoryEntry)
				}
				
				now := time.Now()
				sourceEntry[selectedAnimeName] = models.AnimeHistoryEntry{
					LastEpisodeIdx:  &initialGlobalIndex,
					LastEpisodeName: initialEpisodeName,
					AnimeId:         &selectedAnimeId,
					LastWatched:     &now,
				}
				hist[strings.ToLower(source.Source())] = sourceEntry
				
				if err := history.WriteAnimeHistory(hist); err != nil {
					utils.LogError(Logger, err)
				}
			}

			var stopCh chan struct{}
			if !DisableRPC {
				stopCh = make(chan struct{})
				go utils.UpdateDiscordRPC(socketPath, seasonEpisodeNames, startIndex, selectedAnimeName, SelectedSource, posterURL, timestamp, Logger, stopCh)
			}

			// Playlist tracking için goroutine (startIndex ile, genel indexleri de geç)
			go trackPlaylistProgress(socketPath, strings.ToLower(source.Source()), selectedAnimeName, seasonEpisodeNames, selectedAnimeId, startIndex, seasonEpisodeIndices, Logger)

			// Oynatma işlemi tamamlanana kadar bekle
			err = cmd.Wait()
			if err != nil {
				err = fmt.Errorf("MPV çalışırken hata: %w", err)
				utils.LogError(Logger, err)
				return source, SelectedSource, err
			}

			if stopCh != nil {
				close(stopCh)
			}
			
			// MPV kapandıktan sonra son pozisyonu kontrol et ve menüdeki selectedEpisodeIndex'i güncelle
			time.Sleep(500 * time.Millisecond) // History yazılması için kısa bir bekleme
			updatedHist, err := history.ReadAnimeHistory()
			if err == nil {
				if sourceEntry, ok := updatedHist[strings.ToLower(source.Source())]; ok {
					if animeEntry, ok := sourceEntry[selectedAnimeName]; ok {
						if animeEntry.LastEpisodeIdx != nil {
							// Son izlenen bölümü menü için kaydet
							selectedEpisodeIndex = *animeEntry.LastEpisodeIdx
						}
					}
				}
			}

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
				dlDisplayList, dlNameToIdx := buildSeasonDisplay(episodes, episodeNames)
				var rawChoices []string
				rawChoices, err = ui.MultiSelectList(internal.UiParams{
					Mode:              UiMode,
					List:              &dlDisplayList,
					RofiFlags:         &RofiFlags,
					Label:             "Bölüm seç ",
					SkipAllSeparators: true,
				})
				// Seçilen isimleri orijinal bölüm nesnelerine çevir
				for _, name := range rawChoices {
					if idx, ok := dlNameToIdx[name]; ok {
						choices = append(choices, episodeNames[idx])
					}
				}

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

			// --- İlk bölümden kalite/ses ve altyazı seçeneklerini al ---
			done := make(chan struct{})
			go ui.ShowLoading(internal.UiParams{Mode: UiMode, RofiFlags: &RofiFlags}, "Seçenekler yükleniyor...", done)

			var dlLabels []string
			var dlSubtitles []models.WatchSubtitle
			if len(selectedEpisodes) > 0 {
				firstEp := selectedEpisodes[0]

				if strings.ToLower(SelectedSource) == "anizium" || strings.ToLower(SelectedSource) == "anizium free" {
					// İndirme için tercih filtresi devre dışı → tüm kalite/ses seçeneklerini al
					wpExtra := map[string]interface{}{
						"seasonIndex":           selectedSeasonIndex,
						"episodeIndex":          0,
						"skip_sound_preference": true,
					}
					var dlWatches []models.Watch
					var dlErr error
					if strings.ToLower(SelectedSource) == "anizium free" {
						dlWatches, dlErr = aniziumfree.AniziumFree{}.GetWatchData(models.WatchParams{
							Id: &selectedAnimeID, IsMovie: &isMovie, Url: &firstEp.ID, Extra: &wpExtra,
						})
					} else {
						dlWatches, dlErr = anizium.Anizium{}.GetWatchData(models.WatchParams{
							Id: &selectedAnimeID, IsMovie: &isMovie, Url: &firstEp.ID, Extra: &wpExtra,
						})
					}
					if dlErr == nil && len(dlWatches) > 0 {
						dlLabels = dlWatches[0].Labels
						dlSubtitles = dlWatches[0].Subtitles
					}
				} else {
					// Diğer kaynaklar için normal yol
					firstData, _, firstErr := utils.UpdateWatchAPI(
						strings.ToLower(SelectedSource),
						[]models.Episode{firstEp},
						0, selectedAnimeID, selectedSeasonIndex, selectedFansubIdx, isMovie, &selectedAnimeSlug,
					)
					if firstErr == nil {
						if lbls, ok := firstData["labels"].([]string); ok {
							dlLabels = lbls
						}
					}
				}
			}
			close(done)


			// --- Kalite / Ses seçimi ---
			dlSelectedLabel := ""
			if len(dlLabels) > 1 {
				chosen, selErr := utils.ShowSelection(models.App{UiMode: &UiMode, RofiFlags: &RofiFlags}, dlLabels, "İndirme kalitesi / sesi seç")
				if selErr == nil && slices.Contains(dlLabels, chosen) {
					dlSelectedLabel = chosen
				}
			} else if len(dlLabels) == 1 {
				dlSelectedLabel = dlLabels[0]
			}

			// --- Altyazı seçimi (Anizium) ---
			dlSelectedSubLang := ""
			if (strings.ToLower(SelectedSource) == "anizium" || strings.ToLower(SelectedSource) == "anizium free") && len(dlSubtitles) > 0 {
				subOptions := []string{"Altyazısız"}
				for _, s := range dlSubtitles {
					subOptions = append(subOptions, s.Label)
				}
				chosenSub, subErr := utils.ShowSelection(models.App{UiMode: &UiMode, RofiFlags: &RofiFlags}, subOptions, "Altyazı seç")
				if subErr == nil && chosenSub != "Altyazısız" {
					for _, s := range dlSubtitles {
						if s.Label == chosenSub {
							dlSelectedSubLang = s.Group
							break
						}
					}
				}
			}

			// --- Loading spinner: URL'leri al ---
			done = make(chan struct{})
			go ui.ShowLoading(internal.UiParams{Mode: UiMode, RofiFlags: &RofiFlags}, "URL'ler alınıyor...", done)

			links, err := utils.GetSelectedEpidodesLinks(
				strings.ToLower(SelectedSource),
				selectedEpisodes,
				selectedFansubIdx,
				isMovie,
				&selectedAnimeSlug,
				dlSelectedLabel,
				selectedAnimeID,
			)
			if err != nil {
				close(done)
				ui.ClearScreen()
				fmt.Printf("\033[31m[!] Bölüm URL'leri alınamadı: %s\033[0m\n", err)
				time.Sleep(1500 * time.Millisecond)
				continue
			}
			close(done)
			ui.ClearScreen()

			// --- İndirme döngüsü ---
			successCount := 0
			for _, ep := range selectedEpisodes {
				url, ok := links[ep.Title]
				if !ok {
					fmt.Printf("\033[31m[!] %s için URL bulunamadı.\033[0m\n", ep.Title)
					continue
				}

				// Bölüm numarasını doğrudan ep'den al
				episodeNumber := float64(ep.Number)
				if epNumRaw, ok2 := ep.Extra["episode_num"]; ok2 {
					switch v := epNumRaw.(type) {
					case float64:
						episodeNumber = v
					case int:
						episodeNumber = float64(v)
					}
				}

				seasonNumber := 1
				if snRaw, ok2 := ep.Extra["season_num"]; ok2 {
					if snf, ok3 := snRaw.(float64); ok3 {
						seasonNumber = int(snf)
					}
				}

				// Altyazıyı indir (Anizium, seçildiyse)
				subtitlePath := ""
				if (strings.ToLower(SelectedSource) == "anizium" || strings.ToLower(SelectedSource) == "anizium free") && dlSelectedSubLang != "" {
					// Bu bölümün kendi altyazısını çek
					epData, _, epErr := utils.UpdateWatchAPI(
						strings.ToLower(SelectedSource),
						[]models.Episode{ep},
						0, selectedAnimeID, seasonNumber-1, selectedFansubIdx, isMovie, &selectedAnimeSlug,
					)
					if epErr == nil {
						if subsRaw, ok2 := epData["subtitles"]; ok2 {
							if subs, ok3 := subsRaw.([]models.WatchSubtitle); ok3 {
								for _, s := range subs {
									if s.Group == dlSelectedSubLang {
										if tmpPath, dlErr := anizium.DownloadVTT(s.Link); dlErr == nil {
											subtitlePath = tmpPath
										}
										break
									}
								}
							}
						}
					}
				}

				fmt.Printf("\033[36m⬇  %s indiriliyor...\033[0m\n", ep.Title)
				err = downloader.Download(strings.ToLower(source.Source()), selectedAnimeName, url, episodeNumber, seasonNumber, subtitlePath)
				if err != nil {
					fmt.Printf("\033[31m[!] %s indirilemedi: %s\033[0m\n", ep.Title, err)
				} else {
					fmt.Printf("\033[32m✓  %s indirildi.\033[0m\n", ep.Title)
					successCount++
				}
			}
			fmt.Printf("\n\033[32m%d/%d bölüm başarıyla indirildi.\033[0m\n", successCount, len(selectedEpisodes))
			fmt.Print("\nDevam etmek için ENTER'a basın...")
			fmt.Scanln()


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
	var latestSource string
	var latestTime time.Time

	for sourceName, sourceData := range *cfx.AnimeHistory {
		for animeName, entry := range sourceData {
			if entry.LastWatched != nil && entry.LastWatched.After(latestTime) {
				latestTime = *entry.LastWatched
				latestAnime = animeName
				latestAnimeId = *entry.AnimeId
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

	// Oynatma döngüsüne gir
	_, _, err = PlayAnimeLoop(
		source, *cfx.SelectedSource, episodes, episodeNames,
		selectedAnimeID, selectedAnimeSlug, animeData.Title,
		isMovie, selectedSeasonIndex, *cfx.UiMode, *cfx.RofiFlags,
		posterURL, *cfx.DisableRPC, timestamp, *cfx.AnimeHistory, cfx.Logger,
		false, // autoPlay
	)

	return err
}

// trackPlaylistProgress, playlist oynatılırken pozisyonu takip eder ve history'yi günceller
func trackPlaylistProgress(socketPath, source, animeName string, episodeNames []string, animeId string, startIndex int, globalIndices []int, Logger *models.LogServ) {
	// Callback fonksiyonu: pozisyon değiştiğinde çağrılacak
	onPositionChange := func(position int, episodeName string) {
		// position playlist içindeki pozisyon, bunu genel episode index'e çevir
		globalEpisodeIndex := position
		if position >= 0 && position < len(globalIndices) {
			globalEpisodeIndex = globalIndices[position]
		}
		
		// Playlist modunda direkt olarak history'yi güncelle (süre kontrolü yapmadan)
		hist, err := history.ReadAnimeHistory()
		if err != nil {
			if Logger != nil {
				utils.LogError(Logger, err)
			}
			return
		}
		
		sourceEntry, ok := hist[source]
		if !ok {
			sourceEntry = make(map[string]models.AnimeHistoryEntry)
		}
		
		now := time.Now()
		sourceEntry[animeName] = models.AnimeHistoryEntry{
			LastEpisodeIdx:  &globalEpisodeIndex,
			LastEpisodeName: episodeName,
			AnimeId:         &animeId,
			LastWatched:     &now,
		}
		hist[source] = sourceEntry
		
		if err := history.WriteAnimeHistory(hist); err != nil {
			if Logger != nil {
				utils.LogError(Logger, err)
			}
		}
	}
	
	// Event-based tracking (blocking call)
	player.TrackPlaylistWithEvents(socketPath, animeName, episodeNames, startIndex, onPositionChange)
}

// timeStringToSeconds "HH:MM:SS" formatını saniyeye (float64) çevirir.
func timeStringToSeconds(t string) float64 {
	parts := strings.Split(t, ":")
	if len(parts) != 3 {
		return 0
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	s, _ := strconv.Atoi(parts[2])
	return float64(h*3600 + m*60 + s)
}

// writeChaptersFile — Matroska XML formatında chapter dosyası oluşturur.
// MPV --chapters-file ile okur, progress barında opening tiklerini gösterir.
// writeChapterLuaScript — Lua script ile MPV'ye chapter-list enjekte eder.
// mp.set_property_native MKV'ye gömülü chapter ile aynı şekilde seek bar'da gösterir.
func writeChapterLuaScript(opening *models.OpeningData, ending *models.OpeningData) string {
	startSec := int(timeStringToSeconds(opening.Start))
	endSec := int(timeStringToSeconds(opening.End))

	// Chapters listesini oluştur
	chaptersLua := "local chapters = {\n" +
		"    { title = \"Giris\",   time = 0.0 },\n" +
		fmt.Sprintf("    { title = \"Opening\", time = %d.0 },\n", startSec) +
		fmt.Sprintf("    { title = \"Bolum\",   time = %d.0 },\n", endSec)

	// Ending varsa ekle
	endingOSD := ""
	endingSkip := ""
	if ending != nil {
		endSt := int(timeStringToSeconds(ending.Start))
		endEn := int(timeStringToSeconds(ending.End))
		chaptersLua += fmt.Sprintf("    { title = \"Ending\",  time = %d.0 },\n", endSt)
		chaptersLua += fmt.Sprintf("    { title = \"Bitis\",   time = %d.0 },\n", endEn)
		endingOSD = fmt.Sprintf(`
        elseif title == "Ending" then
            mp.osd_message(">> Ending  [%s - %s]  |  [S] atla", 4)
        elseif title == "Bitis" then
            mp.osd_message(">> Bitis", 3)`, ending.Start, ending.End)
		endingSkip = fmt.Sprintf(`
    -- Ending'deyken S → Bitis'e atla
    elseif current == "Ending" then
        mp.set_property("time-pos", %d.0)
        mp.osd_message(">> Ending atlandı", 2)`, endEn)
	}
	chaptersLua += "}\n"

	lua := "-- anitr-cli chapter inject + skip\n\n" +
		chaptersLua + "\n" +
		"-- file-loaded eventinde chapter-list'i set et\n" +
		"mp.register_event(\"file-loaded\", function()\n" +
		"    mp.set_property_native(\"chapter-list\", chapters)\n" +
		"end)\n\n" +
		"-- Chapter geçişlerinde OSD bildir\n" +
		"mp.observe_property(\"chapter\", \"number\", function(name, val)\n" +
		"    if val == nil then return end\n" +
		"    local list = mp.get_property_native(\"chapter-list\", {})\n" +
		"    if val >= 0 and val < #list then\n" +
		"        local title = list[val + 1].title or \"\"\n" +
		"        if title == \"Opening\" then\n" +
		fmt.Sprintf("            mp.osd_message(\">> Opening  [%s - %s]  |  [A] atla\", 4)\n", opening.Start, opening.End) +
		"        elseif title == \"Bolum\" then\n" +
		"            mp.osd_message(\">> Bolum basladi\", 2)\n" +
		endingOSD + "\n" +
		"        end\n" +
		"    end\n" +
		"end)\n\n" +
		"-- A tusu: aktif chapter'a gore atla (atla = skip, MPV'de bos bir tus)\n" +
		"mp.add_forced_key_binding(\"a\", \"anitr-skip\", function()\n" +
		"    local val = mp.get_property_number(\"chapter\", -1)\n" +
		"    if val < 0 then return end\n" +
		"    local list = mp.get_property_native(\"chapter-list\", {})\n" +
		"    if #list == 0 then return end\n" +
		"    local current = list[math.floor(val) + 1].title or \"\"\n" +
		"    if current == \"Opening\" then\n" +
		fmt.Sprintf("        mp.set_property(\"time-pos\", %d.0)\n", endSec) +
		"        mp.osd_message(\">> Opening atland\u0131\", 2)\n" +
		endingSkip + "\n" +
		"    else\n" +
		"        mp.osd_message(\"[A] atlanacak bolum yok\", 2)\n" +
		"    end\n" +
		"end)\n"

	tmpPath := fmt.Sprintf("%s/anitr_chapter_lua_%d.lua", os.TempDir(), time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, []byte(lua), 0644); err != nil {
		return ""
	}
	return tmpPath
}


