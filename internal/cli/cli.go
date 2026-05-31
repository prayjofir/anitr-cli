package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prayjofir/anitr-cli/internal"
	"github.com/prayjofir/anitr-cli/internal/actions"
	"github.com/prayjofir/anitr-cli/internal/config"
	"github.com/prayjofir/anitr-cli/internal/helpers"
	"github.com/prayjofir/anitr-cli/internal/history"
	"github.com/prayjofir/anitr-cli/internal/models"
	"github.com/prayjofir/anitr-cli/internal/sources/anizium"
	"github.com/prayjofir/anitr-cli/internal/sources/aniziumfree"
	"github.com/prayjofir/anitr-cli/internal/jikan"
	"github.com/prayjofir/anitr-cli/internal/ui"
	"github.com/prayjofir/anitr-cli/internal/ui/tui"
	"github.com/prayjofir/anitr-cli/internal/utils"
)

// Uygulamanın ana fonksiyonu, anime seçimi, oynatma ve hata yönetimini içerir
func AppFunc(cfx *models.App, timestamp time.Time) error {
	for {
		// Anime arama işlemi yapılır
		searchData, animeNames, animeTypes, _, err := SearchAnime(*cfx.Source, *cfx.UiMode, *cfx.RofiFlags, cfx.Logger)

		if errors.Is(err, tui.ErrGoBack) {
			return err
		}

		isMovie := false

		// Kullanıcıdan anime seçimi yapılması istenir
		selectedAnime, isMovie, animeidx := SelectAnime(animeNames, searchData, *cfx.UiMode, isMovie, *cfx.RofiFlags, animeTypes, cfx.Logger)

		if animeidx == -1 {
			continue
		}

		// Loading spinner başlat
		done := make(chan struct{})
		go ui.ShowLoading(internal.UiParams{
			Mode:      *cfx.UiMode,
			RofiFlags: cfx.RofiFlags,
		}, "Yükleniyor...", done)

		// Poster URL'si alınır ve geçersizse varsayılan bir URL kullanılır
		posterURL := selectedAnime.ImageURL
		if !helpers.IsValidImage(posterURL) {
			posterURL = "anitrcli"
		}

		// Seçilen animeye ait ID ve slug alınır
		selectedAnimeID, selectedAnimeSlug := utils.GetAnimeIDs(*cfx.Source, selectedAnime)

		// Anime bölümleri alınır
		episodes, episodeNames, isMovie, selectedSeasonIndex, err := utils.GetEpisodesAndNames(
			*cfx.Source, isMovie, selectedAnimeID, selectedAnimeSlug, selectedAnime.Title,
		)
		// Hata durumunda kullanıcıya seçenek sunulur
		if err != nil {
			// Loading spinner durdur
			close(done)
			// Hatayı logla
			utils.LogError(cfx.Logger, err)

			choice, err := utils.ShowSelection(models.App{UiMode: cfx.UiMode, RofiFlags: cfx.RofiFlags}, []string{"Farklı Anime Ara", "Kaynak Değiştir", "Çık"}, fmt.Sprintf("Hata: %s", err.Error()))
			if err != nil {
				os.Exit(0)
			}

			// Kullanıcının seçimine göre işlem yapılır
			switch choice {
			case "Farklı Anime Ara":
				return nil // Üst döngüye geri dön
			case "Kaynak Değiştir":
				SelectedSource, source := utils.SelectSource(*cfx.UiMode, *cfx.RofiFlags, *cfx.Source, cfx.Logger)
				cfx.SelectedSource = helpers.Ptr(SelectedSource)
				cfx.Source = helpers.Ptr(source)
				return nil
			default:
				os.Exit(0)
			}
		}
		// Loading spinneri durdur
		close(done)

		// Oynatma döngüsüne girilir
		newSource, newSelectedSource, err := actions.PlayAnimeLoop(
			*cfx.Source, *cfx.SelectedSource, episodes, episodeNames,
			selectedAnimeID, selectedAnimeSlug, selectedAnime.Title,
			isMovie, selectedSeasonIndex, *cfx.UiMode, *cfx.RofiFlags,
			posterURL, *cfx.DisableRPC, timestamp, *cfx.AnimeHistory, cfx.Logger,
			false, // autoPlay: normal arama, menü göster
		)

		if errors.Is(err, tui.ErrGoBack) {
			continue // geri butonu → anime arama
		}

		// Bölüm bitmeden çıkıldı → ana menüye dön
		if errors.Is(err, actions.ErrEpisodeNotFinished) {
			return nil
		}

		// Kaynak değiştiyse güncellenir
		if newSource != *cfx.Source || newSelectedSource != *cfx.SelectedSource {
			cfx.Source = &newSource
			cfx.SelectedSource = &newSelectedSource
			return nil
		}

		// Normal bitis → ana menüye dön (tekrar anime arama yerine)
		return nil
	}
}

// Ana menü
func MainMenu(cfx *models.App, timestamp time.Time) {
	// Uygulama açılışında altyazı sunucu keşfini arka planda başlat
	aniziumfree.StartSubtitleDiscovery()

	for {
		// Ekranı temizle
		ui.ClearScreen()

		// Menü seçenekleri
		currentSrc := strings.ToLower(*cfx.SelectedSource)
		favCount := 0
		for _, f := range history.ReadFavorites() {
			if f.Source == currentSrc {
				favCount++
			}
		}
		favLabel := fmt.Sprintf("⭐ Favorilerim (%d)", favCount)
		menuOptions := []string{"Anime Ara", "🌟 Keşfet (Popüler/Sezonluk)", "📋 MAL İzleme Listem", favLabel, "Geçmiş", "Ayarlar", "🗑  Cache Temizle", "Çık"}

		// Anizium seçiliyse giriş seçeneği ekle
		if strings.ToLower(*cfx.SelectedSource) == "anizium" {
			aniCfg, _ := anizium.LoadConfig()
			loginLabel := "Anizium'a Giriş Yap"
			if aniCfg != nil && aniCfg.Token != "" {
				loginLabel = fmt.Sprintf("Anizium: %s ✓", aniCfg.Email)
			}
			menuOptions = append([]string{menuOptions[0]}, append([]string{loginLabel}, menuOptions[1:]...)...)
		}

		// Kullanıcıya mevcut kaynağı göster
		label := fmt.Sprintf("Kaynak: %s", *cfx.SelectedSource)

		// Seçim al
		selectedChoice, err := utils.ShowSelection(*cfx, menuOptions, label)
		if err != nil {
			utils.LogError(cfx.Logger, err)
			continue
		}

		switch selectedChoice {
		case "Anime Ara":
			// Arama-oynatma döngüsüne gir
			if err := AppFunc(cfx, timestamp); err != nil {
				if errors.Is(err, tui.ErrGoBack) {
					continue
				}
				utils.LogError(cfx.Logger, err)
			}

		case favLabel:
			favoritesMenu(cfx, timestamp)

		case "🌟 Keşfet (Popüler/Sezonluk)":
			discoverMenu(cfx, timestamp)

		case "📋 MAL İzleme Listem":
			malWatchlistMenu(cfx, timestamp)

		case "Anizium'a Giriş Yap", "Anizium: " + func() string {
			if cfg, err := anizium.LoadConfig(); err == nil && cfg != nil {
				return cfg.Email + " ✓"
			}
			return ""
		}():
			ui.ClearScreen()
			fmt.Println("Anizium hesabınıza giriş yapın.")
			fmt.Print("Kullanıcı Adı / E-Posta: ")
			var email string
			fmt.Scanln(&email)
			fmt.Print("Şifre: ")
			var password string
			fmt.Scanln(&password)

			if email == "" || password == "" {
				fmt.Println("[!] Kullanıcı adı veya şifre boş.")
				time.Sleep(1500 * time.Millisecond)
				break
			}

			fmt.Println("Giriş yapılıyor...")
			profiles, token, loginErr := anizium.Login(email, password)
			if loginErr != nil {
				fmt.Printf("\033[31m[!] Giriş başarısız: %s\033[0m\n", loginErr)
				fmt.Print("\nMesajı okuduktan sonra devam etmek için ENTER'a basın...")
				var dummy string
				fmt.Scanln(&dummy)
				break
			}

			if len(profiles) == 0 {
				fmt.Println("\033[31m[!] Hesaba bağlı profil bulunamadı.\033[0m")
				fmt.Print("\nMesajı okuduktan sonra devam etmek için ENTER'a basın...")
				var dummy string
				fmt.Scanln(&dummy)
				break
			}

			var selectedProfile anizium.AniziumProfile

			if len(profiles) == 1 {
				selectedProfile = profiles[0]
			} else {
				// Birden fazla profil varsa seçtir
				profileNames := make([]string, len(profiles))
				for i, p := range profiles {
					profileNames[i] = p.Name
				}
				
				chosenName, err := utils.ShowSelection(*cfx, profileNames, "Kullanacağınız Profili Seçin")
				if err != nil {
					fmt.Println("[!] Profil seçimi iptal edildi.")
					time.Sleep(1000 * time.Millisecond)
					break
				}
				
				for _, p := range profiles {
					if p.Name == chosenName {
						selectedProfile = p
						break
					}
				}
			}

			// Profil şifresi varsa sor
			if selectedProfile.Pin != "" {
				ui.ClearScreen()
				fmt.Printf("%s profiline girmek için PIN kodu gerekiyor.\n", selectedProfile.Name)
				fmt.Print("Profil Şifresi (PIN): ")
				var enteredPin string
				fmt.Scanln(&enteredPin)
				
				if enteredPin != selectedProfile.Pin {
					fmt.Println("\033[31m[!] Yanlış PIN girdiniz. Giriş iptal edildi.\033[0m")
					fmt.Print("\nMesajı okuduktan sonra devam etmek için ENTER'a basın...")
					var dummy string
					fmt.Scanln(&dummy)
					break
				}
			}

			// Seçilen profil ile config'i kaydet
			plan := anizium.GetPlanFromUserGet(token)
			aniCfg := &anizium.AniziumConfig{
				Email:  email,
				UserID: selectedProfile.ID,
				Token:  token,
				Plan:   plan,
			}
			
			if err := anizium.SaveConfig(aniCfg); err != nil {
				fmt.Printf("\033[31m[!] Profil kaydedilemedi: %v\033[0m\n", err)
			} else {
				fmt.Printf("\033[32m[✓] Profil seçildi: %s\033[0m\n", selectedProfile.Name)
			}
			time.Sleep(1500 * time.Millisecond)

		case "Kaynak Değiştir":
			SelectedSource, source := utils.SelectSource(*cfx.UiMode, *cfx.RofiFlags, *cfx.Source, cfx.Logger)
			cfx.SelectedSource = helpers.Ptr(SelectedSource)
			cfx.Source = helpers.Ptr(source)
			// Son kullanılan kaynağı config'e kaydet
			_ = config.SaveConfig(filepath.Join(utils.ConfigDir(), "config.json"), func(c *config.Config) {
				c.LastSource = SelectedSource
			})

		case "Geçmiş":
		historyLoop:
			for {
				historySelectedAnime, historyAnimeId, _, err := anitrHistory(internal.UiParams{
					Mode:      *cfx.UiMode,
					RofiFlags: cfx.RofiFlags,
				}, strings.ToLower(*cfx.SelectedSource), cfx.HistoryLimit, cfx.Logger)

				if errors.Is(err, tui.ErrGoBack) {
					break historyLoop
				}
				if err != nil {
					utils.LogError(cfx.Logger, err)
					break historyLoop
				}

				// Loading spinner başlat
				done := make(chan struct{})
				go ui.ShowLoading(internal.UiParams{
					Mode:      *cfx.UiMode,
					RofiFlags: cfx.RofiFlags,
				}, "Yükleniyor...", done)

				var (
					animeSlug string
					animeId   int
				)

				if strings.ToLower(*cfx.SelectedSource) == "openanime" {
					animeSlug = historyAnimeId
				} else {
					animeId, _ = strconv.Atoi(historyAnimeId)
				}

				// Geçmişten isMovie bilgisini al (film hatasını önlemek için)
				histIsMovie := false
				if freshH, ferr := history.ReadAnimeHistory(); ferr == nil {
					if srcMap, ok := freshH[strings.ToLower(*cfx.SelectedSource)]; ok {
						if entry, ok := srcMap[historySelectedAnime]; ok {
							histIsMovie = entry.IsMovie
						}
					}
				}

				// Bölümleri al
				episodes, episodeNames, isMovie, selectedSeasonIndex, err := utils.GetEpisodesAndNames(
					*cfx.Source, histIsMovie, animeId, animeSlug, historySelectedAnime,
				)
				if err != nil {
					close(done) // spinneri durdur
					utils.LogError(cfx.Logger, err)

					choice, choiceErr := utils.ShowSelection(models.App{UiMode: cfx.UiMode, RofiFlags: cfx.RofiFlags}, []string{"Farklı Anime Ara", "Kaynak Değiştir", "Çık"}, fmt.Sprintf("Hata: %s", err.Error()))

					if errors.Is(choiceErr, tui.ErrGoBack) {
						break historyLoop
					}
					if choiceErr != nil {
						os.Exit(0)
					}

					switch choice {
					case "Farklı Anime Ara":
						return
					case "Kaynak Değiştir":
						SelectedSource, source := utils.SelectSource(*cfx.UiMode, *cfx.RofiFlags, *cfx.Source, cfx.Logger)
						cfx.SelectedSource = helpers.Ptr(SelectedSource)
						cfx.Source = helpers.Ptr(source)
						return
					default:
						os.Exit(0)
					}
				}

				// Animenin posterini çek (başarısız olursa varsayılan kullan)
				animePosterURL := "anitrcli"
				if sa, saErr := (*cfx.Source).GetAnimeByID(historyAnimeId); saErr == nil && sa != nil {
					if helpers.IsValidImage(sa.ImageURL) {
						animePosterURL = sa.ImageURL
					}
				} else {
					utils.LogError(cfx.Logger, saErr)
				}

				// Loading spinner durdur
				close(done)

				// Diskten taze geçmiş oku (bellekteki kopya güncel olmayabilir)
				freshHistory, _ := history.ReadAnimeHistory()
				if freshHistory == nil {
					freshHistory = *cfx.AnimeHistory
				}

				// Yeni bölüm bildirimi & son bölüm uyarısı
				if strings.ToLower(*cfx.SelectedSource) == "anizium" || strings.ToLower(*cfx.SelectedSource) == "anizium free" {
					histEntry := freshHistory[strings.ToLower(*cfx.SelectedSource)][historySelectedAnime]
					freshEpCount := len(episodes)

					if histEntry.TotalEpisodeCount > 0 && freshEpCount > histEntry.TotalEpisodeCount {
						newCount := freshEpCount - histEntry.TotalEpisodeCount
						ui.ClearScreen()
						fmt.Printf("\033[32m✨ %s için %d yeni bölüm eklendi! (%d → %d)\033[0m\n",
							historySelectedAnime, newCount, histEntry.TotalEpisodeCount, freshEpCount)
						time.Sleep(2000 * time.Millisecond)
					} else if histEntry.IsFinished && freshEpCount <= histEntry.TotalEpisodeCount {
						ui.ClearScreen()
						fmt.Printf("\033[33m⏳ %s — Son bölümü izlediniz. Yeni bölüm bekleniyor...\033[0m\n", historySelectedAnime)
						time.Sleep(2000 * time.Millisecond)
					}
				}

				// Oynatma döngüsü — geçmişten gelince direkt oynat
				newSource, newSelectedSource, playErr := actions.PlayAnimeLoop(
					*cfx.Source, *cfx.SelectedSource, episodes, episodeNames,
					animeId, animeSlug, historySelectedAnime,
					isMovie, selectedSeasonIndex, *cfx.UiMode, *cfx.RofiFlags,
					animePosterURL, *cfx.DisableRPC, timestamp, freshHistory, cfx.Logger,
					true, // autoPlay: geçmişten gelince direkt başlat
				)

				// Kaynak güncellemesi (her durumda)
				if newSource != nil && newSelectedSource != "" {
					cfx.Source = &newSource
					cfx.SelectedSource = &newSelectedSource
				}

				if errors.Is(playErr, actions.ErrEpisodeNotFinished) {
					// Bölüm bitmeden çıkıldı → geçmiş listesini tekrar göster
					continue historyLoop
				}
				if playErr != nil {
					utils.LogError(cfx.Logger, playErr)
				}
				break historyLoop // normal çıkış → ana menüye dön
			}

		case "Ayarlar":
			settingsMenu(cfx)

		case "🗑  Cache Temizle":
			freedMB := clearMPVCache()
			ui.ClearScreen()
			if freedMB > 0 {
				fmt.Printf("\033[32m[✓] Cache temizlendi: %.1f MB boşaltıldı.\033[0m\n", freedMB)
			} else {
				fmt.Println("[i] Temizlenecek cache dosyası bulunamadı.")
			}
			fmt.Print("Devam etmek için ENTER'a basın...")
			var dummy string
			fmt.Scanln(&dummy)

		case "Çık":
			clearMPVCache() // çıkışta sessizce temizle
			os.Exit(0)
		}
	}
}

// clearMPVCache /tmp altındaki geçici dosyaları ve MPV cache'ini siler, boşaltılan MB'ı döner.
func clearMPVCache() float64 {
	var totalBytes int64

	// Geçici anitr dosyaları
	tmpPatterns := []string{
		filepath.Join(os.TempDir(), "anitr_sub_*.vtt"),
		filepath.Join(os.TempDir(), "anitr-cli*.sock"),
		filepath.Join(os.TempDir(), "mpvsocket_*"),
	}
	for _, pattern := range tmpPatterns {
		matches, _ := filepath.Glob(pattern)
		for _, f := range matches {
			if info, err := os.Stat(f); err == nil {
				totalBytes += info.Size()
			}
			os.Remove(f)
		}
	}

	// ~/.cache/mpv/ — GPU shader cache
	home, err := os.UserHomeDir()
	if err == nil {
		shaderDir := filepath.Join(home, ".cache", "mpv")
		if entries, err := os.ReadDir(shaderDir); err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "shader_") {
					p := filepath.Join(shaderDir, e.Name())
					if info, err := os.Stat(p); err == nil {
						totalBytes += info.Size()
					}
					os.Remove(p)
				}
			}
		}

		// ~/.local/state/mpv/watch_later/ — izleme pozisyonları
		watchDir := filepath.Join(home, ".local", "state", "mpv", "watch_later")
		if entries, err := os.ReadDir(watchDir); err == nil {
			for _, e := range entries {
				p := filepath.Join(watchDir, e.Name())
				if info, err := os.Stat(p); err == nil {
					totalBytes += info.Size()
				}
				os.Remove(p)
			}
		}
	}

	return float64(totalBytes) / (1024 * 1024)
}

func settingsMenu(cfx *models.App) {
	cfg, err := config.LoadConfig(filepath.Join(utils.ConfigDir(), "config.json"))
	if err != nil {
		cfg = &config.Config{}
	}

	// Dosyayı okuma ve yazma modunda açıyoruz, mevcut içeriği sıfırlayarak
	// Config klasörü yoksa oluştur (özellikle Windows'ta ilk açılışta yok olabilir)
	if err := os.MkdirAll(utils.ConfigDir(), 0o755); err != nil {
		utils.LogError(cfx.Logger, fmt.Errorf("config klasörü oluşturulamadı: %w", err))
		return
	}
	f, err := os.OpenFile(filepath.Join(utils.ConfigDir(), "config.json"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		utils.LogError(cfx.Logger, err)
		return
	}
	defer f.Close() // Fonksiyon bitince dosya kapanır

	// JSON yazıcı (Encoder değil)
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")

	// Menüdeki değişikliklerin kaydedilmesi için bir flag
	var changesMade bool

	for {
		// Ekranı temizle
		ui.ClearScreen()

	
		// DisableRPC kontrolü
		var DisableRPCText string
		if cfg.DisableRPC == nil {
			cfg.DisableRPC = helpers.Ptr(false)
		}
		DisableRPCText = fmt.Sprintf("%v", *cfg.DisableRPC)

		menuOptions := []string{
			"İndirme dizinini değiştir : " + cfg.DownloadDir,
			"Geçmiş limitini değiştir : " + fmt.Sprintf("%d", cfg.HistoryLimit),
			"Geçmiş dosyasını aç",
			"RPC'yi devre dışı bırak : " + DisableRPCText,
			"Tercih Edilen Kalite : " + qualityLabel(cfg.PreferredQuality),
			"Tercih Edilen Altyazı : " + subtitleLabel(cfg.PreferredSubtitle),
			"Tercih Edilen Ses : " + soundLabel(cfg.PreferredSound),
			"Geri",
		}

		selectedChoice, err := utils.ShowSelection(*cfx, menuOptions, "Ayarlar")
		if errors.Is(err, tui.ErrGoBack) {
			if changesMade {
				f.Seek(0, io.SeekStart)
				f.Truncate(0)
				if err := encoder.Encode(cfg); err != nil {
					utils.LogError(cfx.Logger, err)
				}
				fmt.Println("Ayarlar başarıyla güncellendi!")
			} else {
				fmt.Println("Değişiklik yapılmadı, ayarlar korunuyor.")
			}
			return
		}
		if err != nil {
			utils.LogError(cfx.Logger, err)
			continue
		}

		// Seçilen menü seçeneğine göre işlem yap
		switch selectedChoice {
		case menuOptions[0]: // İndirme dizinini değiştir
			homeDir := os.Getenv("HOME")
			displayDir := cfg.DownloadDir
			if strings.HasPrefix(cfg.DownloadDir, homeDir) {
				displayDir = "~" + cfg.DownloadDir[len(homeDir):]
			}

			fmt.Printf("Yeni dizin (Enter ile değiştirme) [%s]: ", displayDir)
			var input string
			fmt.Scanln(&input)
			if input != "" {
				if strings.HasPrefix(input, "~") {
					input = filepath.Join(homeDir, input[1:])
				}
				cfg.DownloadDir = input
				changesMade = true
			}

		case menuOptions[1]: // Geçmiş limitini değiştir
			fmt.Print("Yeni geçmiş limitini girin: ")
			var newLimit int
			fmt.Scanln(&newLimit)
			if newLimit >= 0 {
				cfg.HistoryLimit = newLimit
				changesMade = true
			}

		case menuOptions[2]: // Geçmiş dosyasını aç
			path, perr := history.GetHistoryPath()
			if perr != nil {
				utils.LogError(cfx.Logger, perr)
				fmt.Println("Geçmiş yolu alınamadı")
				break
			}
			if err := utils.OpenPath(path); err != nil {
				utils.LogError(cfx.Logger, err)
				fmt.Println("Geçmiş dosyası açılamadı")
			}

		case menuOptions[3]: // RPC'yi devre dışı bırak
			choice, err := utils.ShowSelection(
				models.App{UiMode: cfx.UiMode, RofiFlags: cfx.RofiFlags},
				[]string{"Evet", "Hayır"},
				"Discord Rich Presence devre dışı bırakılsın mı?",
			)

			if errors.Is(err, tui.ErrGoBack) {
				return
			}

			switch strings.ToLower(choice) {
			case "evet":
				cfg.DisableRPC = helpers.Ptr(true)
			case "hayır":
				cfg.DisableRPC = helpers.Ptr(false)
			default:
				cfg.DisableRPC = helpers.Ptr(false)
			}
			changesMade = true

		case menuOptions[4]: // Tercih Edilen Kalite
			qOpts := []string{"4K (En Yüksek)", "2K", "1080p", "720p", "480p", "Sor (Manuel seç)"}
			qChoice, qErr := utils.ShowSelection(
				models.App{UiMode: cfx.UiMode, RofiFlags: cfx.RofiFlags},
				qOpts,
				"Tercih Edilen Kalite",
			)
			if qErr == nil {
				switch qChoice {
				case "4K (En Yüksek)":
					cfg.PreferredQuality = "4K"
				case "2K":
					cfg.PreferredQuality = "2K"
				case "1080p":
					cfg.PreferredQuality = "1080p"
				case "720p":
					cfg.PreferredQuality = "720p"
				case "480p":
					cfg.PreferredQuality = "480p"
				case "Sor (Manuel seç)":
					cfg.PreferredQuality = ""
				}
				changesMade = true
			}

		case menuOptions[5]: // Tercih Edilen Altyazı
			sOpts := []string{
				"🇹🇷 Türkçe",
				"🇬🇧 İngilizce",
				"🇩🇪 Almanca",
				"🇸🇦 Arapça",
				"🇫🇷 Fransızca",
				"🇪🇸 İspanyolca",
				"🇮🇹 İtalyanca",
				"Altyazısız (Sor)",
			}
			sChoice, sErr := utils.ShowSelection(
				models.App{UiMode: cfx.UiMode, RofiFlags: cfx.RofiFlags},
				sOpts,
				"Tercih Edilen Altyazı",
			)
			if sErr == nil {
				switch sChoice {
				case "🇹🇷 Türkçe":
					cfg.PreferredSubtitle = "tr"
				case "🇬🇧 İngilizce":
					cfg.PreferredSubtitle = "en"
				case "🇩🇪 Almanca":
					cfg.PreferredSubtitle = "de"
				case "🇸🇦 Arapça":
					cfg.PreferredSubtitle = "ar"
				case "🇫🇷 Fransızca":
					cfg.PreferredSubtitle = "fr"
				case "🇪🇸 İspanyolca":
					cfg.PreferredSubtitle = "es"
				case "🇮🇹 İtalyanca":
					cfg.PreferredSubtitle = "it"
				case "Altyazısız (Sor)":
					cfg.PreferredSubtitle = ""
				}
				changesMade = true
			}

		case menuOptions[6]: // Tercih Edilen Ses
			sndOpts := []string{
				"🇯🇵 Japonca (Original)",
				"🇹🇷 Türkçe Dublaj",
				"🇬🇧 İngilizce Dublaj",
				"🇨🇳 Çince Dublaj",
				"Sor (Manuel seç)",
			}
			sndChoice, sndErr := utils.ShowSelection(
				models.App{UiMode: cfx.UiMode, RofiFlags: cfx.RofiFlags},
				sndOpts,
				"Tercih Edilen Ses",
			)
			if sndErr == nil {
				switch sndChoice {
				case "🇯🇵 Japonca (Original)":
					cfg.PreferredSound = "original"
				case "🇹🇷 Türkçe Dublaj":
					cfg.PreferredSound = "trdub"
				case "🇬🇧 İngilizce Dublaj":
					cfg.PreferredSound = "endub"
				case "🇨🇳 Çince Dublaj":
					cfg.PreferredSound = "cndub"
				case "Sor (Manuel seç)":
					cfg.PreferredSound = ""
				}
				changesMade = true
			}

		case menuOptions[7]: // Geri
			return
		}

		// Değişiklikleri hemen kaydet
		if changesMade {
			f.Seek(0, io.SeekStart)
			f.Truncate(0)
			if err := encoder.Encode(cfg); err != nil {
				utils.LogError(cfx.Logger, err)
			}
			fmt.Println("Ayarlar güncellendi!")
		}
	}
}

// qualityLabel tercih edilen kaliteyi gösterilebilir hale getirir.
func qualityLabel(q string) string {
	switch q {
	case "4K":
		return "4K"
	case "2K":
		return "2K"
	case "1080p":
		return "1080p"
	case "720p":
		return "720p"
	case "480p":
		return "480p"
	default:
		return "Sor"
	}
}

// subtitleLabel tercih edilen altyazı dilini gösterilebilir hale getirir.
func subtitleLabel(s string) string {
	switch s {
	case "tr":
		return "🇹🇷 Türkçe"
	case "en":
		return "🇬🇧 İngilizce"
	case "de":
		return "🇩🇪 Almanca"
	case "ar":
		return "🇸🇦 Arapça"
	case "fr":
		return "🇫🇷 Fransızca"
	case "es":
		return "🇪🇸 İspanyolca"
	case "it":
		return "🇮🇹 İtalyanca"
	default:
		return "Sor"
	}
}

// soundLabel tercih edilen ses dülini gösterilebilir hale getirir.
func soundLabel(s string) string {
	switch s {
	case "original":
		return "🇯🇵 Japonca"
	case "trdub":
		return "🇹🇷 Türkçe Dublaj"
	case "endub":
		return "🇬🇧 İngilizce Dublaj"
	case "cndub":
		return "🇨🇳 Çince Dublaj"
	default:
		return "Sor"
	}
}

// Anime geçmişini listeleyen fonksiyon
func anitrHistory(params internal.UiParams, source string, historyLimit int, Logger *models.LogServ) (selectedAnime string, animeId string, lastEpisodeIdx int, err error) {
	// Loading spinner başlat
	done := make(chan struct{})
	go ui.ShowLoading(params, "Geçmiş yükleniyor...", done)

	AnimeHistory, readErr := history.ReadAnimeHistory()
	if readErr != nil {
		close(done)
		ui.ClearScreen()
		err = fmt.Errorf("geçmiş bulunamadı")
		fmt.Printf("\033[31m[!] %s\033[0m\n", err.Error())
		utils.LogError(Logger, err)
		time.Sleep(1500 * time.Millisecond)
		return
	}

	sourceData, ok := AnimeHistory[source]
	if !ok || len(sourceData) == 0 {
		close(done)
		ui.ClearScreen()
		err = fmt.Errorf("bu kaynak için geçmiş bulunamadı")
		fmt.Printf("\033[31m[!] %s\033[0m\n", err.Error())
		time.Sleep(1500 * time.Millisecond)
		return
	}

	type item struct {
		Key              string
		AnimeName        string
		AnimeId          string
		Idx              int
		Time             time.Time
		IsFinished       bool
		TotalEpisodeCnt  int
		LastPositionSec  *float64
		LastEpisodeName  string
	}

	var items []item
	for animeName, entry := range sourceData {
		if entry.LastEpisodeName == "" || entry.LastEpisodeIdx == nil || entry.AnimeId == nil || entry.LastWatched == nil || entry.LastWatched.IsZero() {
			continue
		}

		// Durum etiketi
		var statusIcon string
		if entry.IsFinished {
			statusIcon = "✅"
		} else if entry.LastPositionSec != nil && *entry.LastPositionSec > 10 {
			statusIcon = "⏸ "
		} else {
			statusIcon = "▶ "
		}

		// Pozisyon gösterimi (devam eden bölüm için)
		posStr := ""
		if !entry.IsFinished && entry.LastPositionSec != nil && *entry.LastPositionSec > 10 {
			totalSec := int(*entry.LastPositionSec)
			mins := totalSec / 60
			secs := totalSec % 60
			posStr = fmt.Sprintf("  (%d:%02d)", mins, secs)
		}

		key := fmt.Sprintf("%s %s  →  %s%s", statusIcon, animeName, entry.LastEpisodeName, posStr)
		items = append(items, item{
			Key:             key,
			AnimeName:       animeName,
			AnimeId:         *entry.AnimeId,
			Idx:             *entry.LastEpisodeIdx,
			Time:            *entry.LastWatched,
			IsFinished:      entry.IsFinished,
			TotalEpisodeCnt: entry.TotalEpisodeCount,
			LastPositionSec: entry.LastPositionSec,
			LastEpisodeName: entry.LastEpisodeName,
		})
	}

	// en yeniden en eskiye sırala
	sort.Slice(items, func(i, j int) bool {
		return items[i].Time.After(items[j].Time)
	})

	// historyLimit sınırlaması
	effectiveLimit := historyLimit
	if effectiveLimit <= 0 {
		effectiveLimit = 10
	}
	if effectiveLimit > len(items) {
		effectiveLimit = len(items)
	}
	items = items[:effectiveLimit]

	close(done)
	ui.ClearScreen()

	if len(items) == 0 {
		err = fmt.Errorf("bu kaynak için geçmiş bulunamadı")
		fmt.Printf("\033[31m[!] %s\033[0m\n", err.Error())
		time.Sleep(1500 * time.Millisecond)
		return
	}

	var keys []string
	for _, it := range items {
		keys = append(keys, it.Key)
	}

	for {
		// Geçmiş listesini göster
		selectedKey, selErr := ui.SelectionList(internal.UiParams{
			Mode:                 params.Mode,
			List:                 &keys,
			Label:                "Geçmiş",
			RofiFlags:            params.RofiFlags,
			SkipSeasonSeparators: true,
		})
		if selErr != nil {
			err = selErr
			return
		}

		// Seçilen animei bul
		var chosen *item
		for i := range items {
			if items[i].Key == selectedKey {
				chosen = &items[i]
				break
			}
		}
		if chosen == nil {
			err = fmt.Errorf("seçilen anime bulunamadı")
			return
		}

		// Alt menü: Devam Et / Geçmişten Sil / Geri
		subMenuItems := []string{"▶  Devam Et", "🗑  Geçmişten Sil", "← Geri"}
		subChoice, subErr := utils.ShowSelection(
			models.App{UiMode: &params.Mode, RofiFlags: params.RofiFlags},
			subMenuItems,
			chosen.AnimeName,
		)
		if subErr != nil {
			err = subErr
			return
		}

		switch subChoice {
		case "🗑  Geçmişten Sil":
			if delErr := history.DeleteAnimeHistory(source, chosen.AnimeName); delErr != nil {
				utils.LogError(Logger, delErr)
				fmt.Printf("\033[31m[!] Silinemedi: %v\033[0m\n", delErr)
				time.Sleep(1000 * time.Millisecond)
			} else {
				fmt.Printf("\033[32m[✓] %s geçmişten silindi.\033[0m\n", chosen.AnimeName)
				time.Sleep(800 * time.Millisecond)
			}
			// Listeyi güncelle ve tekrar göster
			AnimeHistory, _ = history.ReadAnimeHistory()
			sourceData = AnimeHistory[source]
			items = nil
			keys = nil
			for animeName, entry := range sourceData {
				if entry.LastEpisodeName == "" || entry.LastEpisodeIdx == nil || entry.AnimeId == nil || entry.LastWatched == nil || entry.LastWatched.IsZero() {
					continue
				}
				var statusIcon string
				if entry.IsFinished {
					statusIcon = "✅"
				} else if entry.LastPositionSec != nil && *entry.LastPositionSec > 10 {
					statusIcon = "⏸ "
				} else {
					statusIcon = "▶ "
				}
				posStr := ""
				if !entry.IsFinished && entry.LastPositionSec != nil && *entry.LastPositionSec > 10 {
					totalSec := int(*entry.LastPositionSec)
					mins := totalSec / 60
					secs := totalSec % 60
					posStr = fmt.Sprintf("  (%d:%02d)", mins, secs)
				}
				k := fmt.Sprintf("%s %s  →  %s%s", statusIcon, animeName, entry.LastEpisodeName, posStr)
				items = append(items, item{
					Key:             k,
					AnimeName:       animeName,
					AnimeId:         *entry.AnimeId,
					Idx:             *entry.LastEpisodeIdx,
					Time:            *entry.LastWatched,
					IsFinished:      entry.IsFinished,
					TotalEpisodeCnt: entry.TotalEpisodeCount,
					LastPositionSec: entry.LastPositionSec,
					LastEpisodeName: entry.LastEpisodeName,
				})
			}
			sort.Slice(items, func(i, j int) bool { return items[i].Time.After(items[j].Time) })
			if effectiveLimit > len(items) {
				effectiveLimit = len(items)
			}
			if effectiveLimit > 0 {
				items = items[:effectiveLimit]
			}
			for _, it := range items {
				keys = append(keys, it.Key)
			}
			if len(items) == 0 {
				err = fmt.Errorf("geçmiş boş")
				return
			}
			continue

		case "← Geri":
			continue // liste tekrar gösterilir

		default: // "▶  Devam Et"
			selectedAnime = chosen.AnimeName
			animeId = chosen.AnimeId
			lastEpisodeIdx = chosen.Idx
			return
		}
	}
}

// Kullanıcıdan arama girdisi alır ve API üzerinden sonuçları getirir
func SearchAnime(source models.AnimeSource, UiMode string, RofiFlags string, Logger *models.LogServ) ([]models.Anime, []string, []string, map[string]models.Anime, error) {
	for {
		// Arama geçmişini oku
		recentSearches := history.ReadSearchHistory()

		var query string
		if len(recentSearches) > 0 {
			// Seçim listesi: Yeni Arama + son aramalar
			options := append([]string{"🔍 Yeni Arama", "🗑  Gecmisi Temizle"}, recentSearches...)
			selected, selErr := utils.ShowSelection(
				models.App{UiMode: &UiMode, RofiFlags: &RofiFlags},
				options, "Anime ara",
			)
			if errors.Is(selErr, tui.ErrGoBack) {
				return nil, nil, nil, nil, selErr
			}
			if selErr != nil {
				return nil, nil, nil, nil, selErr
			}
			if selected == "🗑  Gecmisi Temizle" {
				history.ClearSearchHistory()
				continue
			}
			if selected == "🔍 Yeni Arama" {
				var err error
				query, err = ui.InputFromUser(internal.UiParams{Mode: UiMode, RofiFlags: &RofiFlags, Label: "Anime ara "})
				if errors.Is(err, tui.ErrGoBack) {
					continue // geçmiş listesine dön
				}
				if err != nil {
					continue
				}
			} else {
				query = selected // geçmişten seçildi
			}
		} else {
			// Geçmiş yok → direkt input
			var err error
			query, err = ui.InputFromUser(internal.UiParams{Mode: UiMode, RofiFlags: &RofiFlags, Label: "Anime ara "})
			if errors.Is(err, tui.ErrGoBack) {
				return nil, nil, nil, nil, err
			}
			if err != nil {
				continue
			}
		}

		utils.FailIfErr(internal.UiParams{
			Mode:      UiMode,
			RofiFlags: &RofiFlags,
		}, nil, Logger)

		// Loading spinner başlat
		done := make(chan struct{})
		go ui.ShowLoading(internal.UiParams{
			Mode:      UiMode,
			RofiFlags: &RofiFlags,
		}, "Aranıyor...", done)

		// API üzerinden arama yap
		// API üzerinden arama yap (Kaynak ve Jikan eşzamanlı)
		var searchData []models.Anime
		var sourceErr error
		var jikanResults []jikan.AnimeBasic

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			searchData, sourceErr = source.GetSearchData(query)
		}()
		go func() {
			defer wg.Done()
			jikanResults, _ = jikan.SearchAnime(query)
		}()
		wg.Wait()

		if sourceErr != nil {
			close(done)      // spinneri durdur
			ui.ClearScreen() // ekranı temizle

			ui.ShowError(internal.UiParams{
				Mode:      UiMode,
				RofiFlags: &RofiFlags,
			}, fmt.Sprintf(
				"%s kaynağına erişilemedi."+"\n\n"+
					"Olası nedenler:\n"+
					"1. VPN açık olabilir\n"+
					"2. Proxy ayarlarından kaynaklı olabilir\n"+
					"3. İnternete bağlı olmayabilirsiniz\n"+
					"4. Bunların hiçbiri değilse API taşınmış olabilir, lütfen GitHub'da issue açarak hatayı bize bildirin.", strings.ToLower(source.Source())))

			os.Exit(1)
		}
		// Başarılı aramayı geçmişe kaydet
		history.SaveSearchQuery(query)

		// Arama sonucu yoksa bilgilendir
		if searchData == nil {
			close(done)
			ui.ClearScreen()
			fmt.Printf("\033[31m[!] Arama sonucu bulunamadı!\033[0m")
			time.Sleep(1500 * time.Millisecond)
			continue
		}

		// Arama sonuçlarını işleyip ilgili listeleri oluştur
		animeNames := make([]string, 0, len(searchData))
		animeTypes := make([]string, 0, len(searchData))
		animeMap := make(map[string]models.Anime)

		for _, item := range searchData {
			displayTitle := item.Title

			if matched := jikan.MatchAnimeTitle(item.Title, jikanResults); matched != nil {
				jikan.SetMalIDCache(item.Title, matched.MalID)
				scoreStr := utils.FormatAnimeDetails(matched.Score, matched.Year, matched.Aired.From, matched.Genres)
				displayTitle = item.Title + scoreStr
			} else {
				// Eşleşmeyenleri tekil olarak ara (Jikan Rate Limit: 3 req/sec)
				time.Sleep(334 * time.Millisecond)
				specificResults, err := jikan.SearchAnime(item.Title)
				if err == nil && len(specificResults) > 0 {
					matchedSpecific := jikan.MatchAnimeTitle(item.Title, specificResults)
					if matchedSpecific != nil {
						jikan.SetMalIDCache(item.Title, matchedSpecific.MalID)
						scoreStr := utils.FormatAnimeDetails(matchedSpecific.Score, matchedSpecific.Year, matchedSpecific.Aired.From, matchedSpecific.Genres)
						displayTitle = item.Title + scoreStr
					} else {
						jikan.SetMalIDCache(item.Title, specificResults[0].MalID)
						scoreStr := utils.FormatAnimeDetails(specificResults[0].Score, specificResults[0].Year, specificResults[0].Aired.From, specificResults[0].Genres)
						displayTitle = item.Title + scoreStr
					}
				}
			}

			animeNames = append(animeNames, displayTitle)
			animeMap[displayTitle] = item

			// Anime türünü belirle (tv veya movie)
			if item.TitleType != nil {
				ttype := item.TitleType
				if strings.ToLower(*ttype) == "movie" {
					animeTypes = append(animeTypes, "movie")
				} else {
					animeTypes = append(animeTypes, "tv")
				}
			}
		}

		// Loading spinneri durdur
		close(done)

		return searchData, animeNames, animeTypes, animeMap, nil
	}
}

// Kullanıcının seçtiği animeyi belirler
func SelectAnime(animeNames []string, searchData []models.Anime, UiMode string, isMovie bool, RofiFlags string, animeTypes []string, Logger *models.LogServ) (models.Anime, bool, int) {
	for {
		ui.ClearScreen()

		// Kullanıcıdan anime seçimi al
		selectedAnimeName, err := utils.ShowSelection(models.App{UiMode: &UiMode, RofiFlags: &RofiFlags}, animeNames, "Anime seç ")

		if errors.Is(err, tui.ErrGoBack) {
			// kullanıcı ESC bastı → fonksiyonu çağıran yere geri dön
			return models.Anime{}, false, -1
		}

		utils.FailIfErr(internal.UiParams{
			Mode:      UiMode,
			RofiFlags: &RofiFlags,
		}, err, Logger)

		// Geçerli bir anime ismi mi kontrol et
		if !slices.Contains(animeNames, selectedAnimeName) {
			continue
		}

		// Seçilen animeyi bul
		selectedIndex := slices.Index(animeNames, selectedAnimeName)
		selectedAnime := searchData[selectedIndex]

		// Anime türü (movie / tv) güncelleniyor
		if len(animeTypes) > 0 {
			selectedAnimeType := animeTypes[selectedIndex]
			isMovie = selectedAnimeType == "movie"
		}

		return selectedAnime, isMovie, selectedIndex
	}
}

// favoritesMenu — favori anime listesini gösterir ve seçileni oynatır.
func favoritesMenu(cfx *models.App, timestamp time.Time) {
	for {
		currentSrc := strings.ToLower(*cfx.SelectedSource)
		allFavs := history.ReadFavorites()

		// Sadece mevcut kaynağa ait favoriler
		favs := make([]history.FavoriteEntry, 0, len(allFavs))
		for _, f := range allFavs {
			if f.Source == currentSrc {
				favs = append(favs, f)
			}
		}

		if len(favs) == 0 {
			ui.ClearScreen()
			fmt.Println("\033[33m⭐ Favori listeniz boş.\033[0m")
			fmt.Print("Ana menüye dönmek için ENTER'a basın...")
			fmt.Scanln()
			return
		}

		// Liste: başlıklar + sil seçeneği
		options := make([]string, 0, len(favs)+1)
		for _, f := range favs {
			src := strings.ToUpper(f.Source[:1]) + f.Source[1:]
			
			// Genre struct array oluştur
			var genres []struct {
				Name string `json:"name"`
			}
			for _, g := range f.Genres {
				genres = append(genres, struct {
					Name string `json:"name"`
				}{Name: g})
			}
			
			scoreStr := utils.FormatAnimeDetails(f.Score, f.Year, f.Aired, genres)
			options = append(options, fmt.Sprintf("%s%s  [%s]", f.Title, scoreStr, src))
		}
		options = append(options, "🗑  Favori Sil")

		selected, err := utils.ShowSelection(
			models.App{UiMode: cfx.UiMode, RofiFlags: cfx.RofiFlags},
			options, "⭐ Favoriler",
		)
		if errors.Is(err, tui.ErrGoBack) || err != nil {
			return
		}

		if selected == "🗑  Favori Sil" {
			// Silme menüsü
			delOptions := make([]string, len(favs))
			for i, f := range favs {
				delOptions[i] = f.Title
			}
			toDelete, delErr := utils.ShowSelection(
				models.App{UiMode: cfx.UiMode, RofiFlags: cfx.RofiFlags},
				delOptions, "Silinecek favoriyi seç",
			)
			if delErr == nil && toDelete != "" {
				for _, f := range favs {
					if f.Title == toDelete {
						history.RemoveFavorite(f.ID, f.Source)
						break
					}
				}
			}
			continue
		}

		// Seçilen favorinin indeksini bul
		selectedIdx := -1
		for i, opt := range options {
			if opt == selected {
				selectedIdx = i
				break
			}
		}
		if selectedIdx < 0 || selectedIdx >= len(favs) {
			continue
		}

		fav := favs[selectedIdx]

		// Bölümleri yükle
		done := make(chan struct{})
		go ui.ShowLoading(internal.UiParams{
			Mode:      *cfx.UiMode,
			RofiFlags: cfx.RofiFlags,
		}, "Yükleniyor...", done)

		animeId := 0
		animeSlug := ""
		if fav.Source == "animecix" || strings.HasPrefix(fav.Source, "anizium") {
			animeId, _ = strconv.Atoi(fav.ID)
		} else {
			animeSlug = fav.ID
		}

		episodes, episodeNames, isMovie, seasonIdx, epErr := utils.GetEpisodesAndNames(
			*cfx.Source, fav.IsMovie, animeId, animeSlug, fav.Title,
		)
		close(done)

		if epErr != nil {
			utils.LogError(cfx.Logger, epErr)
			ui.ClearScreen()
			fmt.Printf("\033[31m[!] Bölümler alınamadı: %s\033[0m\n", epErr)
			time.Sleep(2 * time.Second)
			continue
		}

		freshHistory, _ := history.ReadAnimeHistory()
		if freshHistory == nil {
			freshHistory = *cfx.AnimeHistory
		}

		actions.PlayAnimeLoop(
			*cfx.Source, *cfx.SelectedSource, episodes, episodeNames,
			animeId, animeSlug, fav.Title,
			isMovie, seasonIdx, *cfx.UiMode, *cfx.RofiFlags,
			"anitrcli", *cfx.DisableRPC, timestamp, freshHistory, cfx.Logger,
			false,
		)
	}
}
func discoverMenu(cfx *models.App, timestamp time.Time) {
	menuOpts := []string{"🏆 Popüler Animeler (Top)", "🌸 Bu Sezonun Animeleri (Airing)", "Geri"}
	choice, err := utils.ShowSelection(*cfx, menuOpts, "Keşfet")
	if err != nil || choice == "Geri" {
		return
	}

	var list []jikan.AnimeBasic
	var fetchErr error

	ui.ClearScreen()
	fmt.Println("Yükleniyor...")
	if choice == "🏆 Popüler Animeler (Top)" {
		list, fetchErr = jikan.GetTopAnime()
	} else {
		list, fetchErr = jikan.GetSeasonalAnime()
	}

	if fetchErr != nil {
		fmt.Printf("[!] Hata: %s\n", fetchErr)
		time.Sleep(2 * time.Second)
		return
	}

	if len(list) == 0 {
		fmt.Println("[!] Anime bulunamadı.")
		time.Sleep(2 * time.Second)
		return
	}

	var titles []string
	animeMap := make(map[string]jikan.AnimeBasic)
	for _, a := range list {
		jikan.SetMalIDCache(a.Title, a.MalID)
		scoreStr := utils.FormatAnimeDetails(a.Score, a.Year, a.Aired.From, a.Genres)

		displayTitle := fmt.Sprintf("%s%s", a.Title, scoreStr)
		titles = append(titles, displayTitle)
		animeMap[displayTitle] = a
	}
	titles = append(titles, "Geri")

	selectedDisplay, selErr := utils.ShowSelection(*cfx, titles, choice)
	if selErr != nil || selectedDisplay == "Geri" {
		return
	}

	selectedAnimeBasic := animeMap[selectedDisplay]
	autoSearch(cfx, selectedAnimeBasic.Title, timestamp)
}

func malWatchlistMenu(cfx *models.App, timestamp time.Time) {
	cfg, err := config.LoadConfig(filepath.Join(helpers.ConfigDir(), "config.json"))
	if err != nil || cfg.MALUsername == "" {
		fmt.Println("[!] MAL Kullanıcı Adı ayarlanmamış. Lütfen Ayarlar menüsünden belirleyin.")
		time.Sleep(2 * time.Second)
		return
	}

	ui.ClearScreen()
	fmt.Println("Listeniz çekiliyor, lütfen bekleyin...")
	
	list, fetchErr := jikan.GetUserWatchlist(cfg.MALUsername)
	if fetchErr != nil {
		fmt.Printf("[!] %s\n", fetchErr)
		time.Sleep(4 * time.Second)
		return
	}

	if len(list) == 0 {
		fmt.Println("[!] İzlediğiniz bir anime bulunamadı veya listeniz gizli.")
		time.Sleep(2 * time.Second)
		return
	}

	var titles []string
	animeMap := make(map[string]jikan.AnimeBasic)
	for _, a := range list {
		jikan.SetMalIDCache(a.Title, a.MalID)
		scoreStr := utils.FormatAnimeDetails(a.Score, a.Year, a.Aired.From, a.Genres)

		displayTitle := fmt.Sprintf("%s%s", a.Title, scoreStr)
		titles = append(titles, displayTitle)
		animeMap[displayTitle] = a
	}
	titles = append(titles, "Geri")

	selectedDisplay, selErr := utils.ShowSelection(*cfx, titles, "MAL İzleme Listem")
	if selErr != nil || selectedDisplay == "Geri" {
		return
	}

	selectedAnimeBasic := animeMap[selectedDisplay]
	autoSearch(cfx, selectedAnimeBasic.Title, timestamp)
}

func autoSearch(cfx *models.App, query string, timestamp time.Time) {
	done := make(chan struct{})
	go ui.ShowLoading(internal.UiParams{
		Mode:      *cfx.UiMode,
		RofiFlags: cfx.RofiFlags,
	}, fmt.Sprintf("'%s' aranıyor...", query), done)

	var searchData []models.Anime
	var err error

	searchData, err = (*cfx.Source).GetSearchData(query)

	// Bulamazsa ilk iki kelimeyle ara (AnimeciX sezonları tek sayfada topladığı için)
	if err != nil || len(searchData) == 0 {
		words := strings.Fields(strings.ReplaceAll(query, ":", " "))
		if len(words) > 1 {
			baseTitle := strings.Join(words[:2], " ")
			searchData, err = (*cfx.Source).GetSearchData(baseTitle)
		}
	}
	close(done)

	if err != nil || len(searchData) == 0 {
		ui.ClearScreen()
		fmt.Printf("\033[31m[!] '%s' bulunamadı.\033[0m\n", query)
		time.Sleep(2 * time.Second)
		return
	}

	var wg sync.WaitGroup
	var jikanResults []jikan.AnimeBasic
	wg.Add(1)
	go func() {
		defer wg.Done()
		jikanResults, _ = jikan.SearchAnime(query)
	}()
	wg.Wait()

	animeNames := make([]string, len(searchData))
	animeTypes := make([]string, len(searchData))
	for i, item := range searchData {
		displayTitle := item.Title
		
		if matched := jikan.MatchAnimeTitle(item.Title, jikanResults); matched != nil {
			jikan.SetMalIDCache(item.Title, matched.MalID)
			scoreStr := utils.FormatAnimeDetails(matched.Score, matched.Year, matched.Aired.From, matched.Genres)
			displayTitle = item.Title + scoreStr
		} else {
			// Eşleşmeyenleri tekil olarak ara (Jikan Rate Limit: 3 req/sec)
			time.Sleep(334 * time.Millisecond)
			specificResults, err := jikan.SearchAnime(item.Title)
			if err == nil && len(specificResults) > 0 {
				matchedSpecific := jikan.MatchAnimeTitle(item.Title, specificResults)
				if matchedSpecific != nil {
					jikan.SetMalIDCache(item.Title, matchedSpecific.MalID)
					scoreStr := utils.FormatAnimeDetails(matchedSpecific.Score, matchedSpecific.Year, matchedSpecific.Aired.From, matchedSpecific.Genres)
					displayTitle = item.Title + scoreStr
				} else {
					jikan.SetMalIDCache(item.Title, specificResults[0].MalID)
					scoreStr := utils.FormatAnimeDetails(specificResults[0].Score, specificResults[0].Year, specificResults[0].Aired.From, specificResults[0].Genres)
					displayTitle = item.Title + scoreStr
				}
			}
		}
		
		animeNames[i] = displayTitle

		if item.TitleType != nil {
			ttype := item.TitleType
			if strings.ToLower(*ttype) == "movie" {
				animeTypes[i] = "movie"
			} else {
				animeTypes[i] = "tv"
			}
		} else {
			animeTypes[i] = "tv"
		}
	}

	// Just use the first result automatically or let the user choose if multiple?
	// It's better to let them choose in case the scraper returns multiple seasons.
	selectedAnime, isMovie, animeidx := SelectAnime(animeNames, searchData, *cfx.UiMode, false, *cfx.RofiFlags, animeTypes, cfx.Logger)
	if animeidx == -1 {
		return
	}

	// Loading spinner başlat
	done2 := make(chan struct{})
	go ui.ShowLoading(internal.UiParams{
		Mode:      *cfx.UiMode,
		RofiFlags: cfx.RofiFlags,
	}, "Yükleniyor...", done2)

	posterURL := selectedAnime.ImageURL
	if !helpers.IsValidImage(posterURL) {
		posterURL = "anitrcli"
	}

	selectedAnimeID, selectedAnimeSlug := utils.GetAnimeIDs(*cfx.Source, selectedAnime)

	episodes, episodeNames, isMovie, selectedSeasonIndex, err := utils.GetEpisodesAndNames(
		*cfx.Source, isMovie, selectedAnimeID, selectedAnimeSlug, selectedAnime.Title,
	)
	close(done2)

	if err != nil {
		utils.LogError(cfx.Logger, err)
		return
	}

	actions.PlayAnimeLoop(
		*cfx.Source, *cfx.SelectedSource, episodes, episodeNames,
		selectedAnimeID, selectedAnimeSlug, selectedAnime.Title,
		isMovie, selectedSeasonIndex, *cfx.UiMode, *cfx.RofiFlags,
		posterURL, *cfx.DisableRPC, timestamp, *cfx.AnimeHistory, cfx.Logger,
		false,
	)
}
