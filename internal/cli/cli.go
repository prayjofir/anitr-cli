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
	"time"

	"github.com/axrona/anitr-cli/internal"
	"github.com/axrona/anitr-cli/internal/actions"
	"github.com/axrona/anitr-cli/internal/config"
	"github.com/axrona/anitr-cli/internal/helpers"
	"github.com/axrona/anitr-cli/internal/history"
	"github.com/axrona/anitr-cli/internal/models"
	"github.com/axrona/anitr-cli/internal/ui"
	"github.com/axrona/anitr-cli/internal/ui/tui"
	"github.com/axrona/anitr-cli/internal/utils"
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
		)

		if errors.Is(err, tui.ErrGoBack) {
			continue
		}

		// Kaynak değiştiyse güncellenir
		if newSource != *cfx.Source || newSelectedSource != *cfx.SelectedSource {
			cfx.Source = &newSource
			cfx.SelectedSource = &newSelectedSource
			return nil
		}
	}
}

// Ana menü
func MainMenu(cfx *models.App, timestamp time.Time) {
	for {
		// Ekranı temizle
		ui.ClearScreen()

		// Menü seçenekleri
		menuOptions := []string{"Anime Ara", "Kaynak Değiştir", "Geçmiş", "Ayarlar", "Çık"}

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

		case "Kaynak Değiştir":
			SelectedSource, source := utils.SelectSource(*cfx.UiMode, *cfx.RofiFlags, *cfx.Source, cfx.Logger)
			cfx.SelectedSource = helpers.Ptr(SelectedSource)
			cfx.Source = helpers.Ptr(source)

		case "Geçmiş":
			historySelectedAnime, historyAnimeId, _, err := anitrHistory(internal.UiParams{
				Mode:      *cfx.UiMode,
				RofiFlags: cfx.RofiFlags,
			}, strings.ToLower(*cfx.SelectedSource), cfx.HistoryLimit, cfx.Logger)

			if errors.Is(err, tui.ErrGoBack) {
				continue
			}
			if err != nil {
				utils.LogError(cfx.Logger, err)
				break
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
				animeId, err = strconv.Atoi(historyAnimeId)
			}

			// Bölümleri al
			episodes, episodeNames, isMovie, selectedSeasonIndex, err := utils.GetEpisodesAndNames(
				*cfx.Source, false, animeId, animeSlug, historySelectedAnime,
			)
			if err != nil {
				close(done) // spinneri durdur

				utils.LogError(cfx.Logger, err)

				choice, err := utils.ShowSelection(models.App{UiMode: cfx.UiMode, RofiFlags: cfx.RofiFlags}, []string{"Farklı Anime Ara", "Kaynak Değiştir", "Çık"}, fmt.Sprintf("Hata: %s", err.Error()))

				if errors.Is(err, tui.ErrGoBack) {
					continue
				}

				if err != nil {
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

			// Animenin verilerini çek
			source := *cfx.Source
			selectedAnime, err := source.GetAnimeByID(historyAnimeId)
			if err != nil {
				close(done) // spinneri durdur
				utils.LogError(cfx.Logger, err)
				return
			}

			// Poster URL al
			posterURL := selectedAnime.ImageURL
			if !helpers.IsValidImage(posterURL) {
				posterURL = "anitrcli"
			}

			// Loading spinner durdur
			close(done)

			// Oynatma döngüsü
			newSource, newSelectedSource, err := actions.PlayAnimeLoop(
				*cfx.Source, *cfx.SelectedSource, episodes, episodeNames,
				animeId, animeSlug, historySelectedAnime,
				isMovie, selectedSeasonIndex, *cfx.UiMode, *cfx.RofiFlags,
				posterURL, *cfx.DisableRPC, timestamp, *cfx.AnimeHistory, cfx.Logger,
			)
			if err != nil {
				utils.LogError(cfx.Logger, err)
			}

			// kaynak güncellemesi olursa güncelle
			if newSource != nil && newSelectedSource != "" {
				cfx.Source = &newSource
				cfx.SelectedSource = &newSelectedSource
			}

		case "Ayarlar":
			settingsMenu(cfx)

		case "Çık":
			os.Exit(0)
		}
	}
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

		var SelectedSourceText string
		if cfx.SelectedSource != nil {
			SelectedSourceText = *cfx.SelectedSource
		} else {
			SelectedSourceText = "Seçili kaynak yok"
		}

		// DisableRPC kontrolü: Nil ise false olarak ayarla
		var DisableRPCText string
		if cfg.DisableRPC == nil {
			cfg.DisableRPC = helpers.Ptr(false)
		}
		DisableRPCText = fmt.Sprintf("%v", *cfg.DisableRPC)

		menuOptions := []string{
			"İndirme dizinini değiştir : " + cfg.DownloadDir,
			"Varsayılan kaynağı değiştir : " + SelectedSourceText,
			"Geçmiş limitini değiştir : " + fmt.Sprintf("%d", cfg.HistoryLimit),
			"RPC'yi devre dışı bırak : " + DisableRPCText,
			"Geri",
		}

		selectedChoice, err := utils.ShowSelection(*cfx, menuOptions, "Ayarlar")
		if errors.Is(err, tui.ErrGoBack) {
			// Menüden çıkıldığında kaydetme işlemi yap
			if changesMade {
				// Dosyaya yazma işlemi sadece değişiklik yapıldıysa yapılacak
				f.Seek(0, io.SeekStart) // Dosya pointer'ını başa al
				f.Truncate(0)           // Dosyayı temizle
				if err := encoder.Encode(cfg); err != nil {
					utils.LogError(cfx.Logger, err)
				}
				fmt.Println("Ayarlar başarıyla güncellendi!")
			} else {
				// Değişiklik yapılmamışsa dosyayı yazma
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
				changesMade = true // Flag'i true yapıyoruz, çünkü değişiklik yapıldı
			}

		case menuOptions[1]: // Kaynak değiştir
			SelectedSourceName, SelectedSource := utils.SelectSource(*cfx.UiMode, *cfx.RofiFlags, *cfx.Source, cfx.Logger)
			cfx.SelectedSource = &SelectedSourceName
			cfx.Source = &SelectedSource
			cfg.DefaultSource = SelectedSourceName
			changesMade = true

		case menuOptions[2]: // Geçmiş limitini değiştir
			fmt.Print("Yeni geçmiş limitini girin: ")
			var newLimit int
			fmt.Scanln(&newLimit)
			if newLimit >= 0 {
				cfg.HistoryLimit = newLimit
				changesMade = true
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

		case menuOptions[4]: // Geri
			return
		}

		// Değişiklikleri hemen kaydet
		if changesMade {
			// Dosyaya yazma işlemi manuel olarak yapılır
			f.Seek(0, io.SeekStart) // Dosya pointer'ını başa al
			f.Truncate(0)           // Dosyayı temizle
			if err := encoder.Encode(cfg); err != nil {
				utils.LogError(cfx.Logger, err)
			}
			fmt.Println("Ayarlar güncellendi!")
		}
	}
}

// Anime geçmişini listeleyen fonksiyon
func anitrHistory(params internal.UiParams, source string, historyLimit int, Logger *models.LogServ) (selectedAnime string, animeId string, lastEpisodeIdx int, err error) {
	// Loading spinner başlat
	done := make(chan struct{})
	go ui.ShowLoading(params, "Geçmiş yükleniyor...", done)

	AnimeHistory, readErr := history.ReadAnimeHistory()
	if readErr != nil {
		close(done)      // spinner'ı kapat
		ui.ClearScreen() // ekranı temizle
		err = fmt.Errorf("Geçmiş bulunamadı")
		fmt.Printf("\033[31m[!] %s\033[0m\n", err.Error())
		utils.LogError(Logger, err)
		time.Sleep(1500 * time.Millisecond)
		return
	}

	sourceData, ok := AnimeHistory[source]
	if !ok || len(sourceData) == 0 {
		close(done)      // spinner'ı kapat
		ui.ClearScreen() // ekranı temizle
		err = fmt.Errorf("Bu kaynak için geçmiş bulunamadı")
		fmt.Printf("\033[31m[!] %s\033[0m\n", err.Error())
		time.Sleep(1500 * time.Millisecond)
		return
	}

	// slice'e taşı
	type item struct {
		Key       string
		AnimeName string
		AnimeId   string
		Idx       int
		Time      time.Time
	}

	var items []item
	for animeName, entry := range sourceData {
		if entry.LastEpisodeName == "" || entry.LastEpisodeIdx == nil || entry.AnimeId == nil || entry.LastWatched == nil || entry.LastWatched.IsZero() {
			continue
		}
		key := fmt.Sprintf("%s %s", animeName, entry.LastEpisodeName)
		items = append(items, item{
			Key:       key,
			AnimeName: animeName,
			AnimeId:   *entry.AnimeId,
			Idx:       *entry.LastEpisodeIdx,
			Time:      *entry.LastWatched,
		})
	}

	// en yeniden en eskiye sırala
	sort.Slice(items, func(i, j int) bool {
		return items[i].Time.After(items[j].Time)
	})

	// historyLimit ile sınırla
	if historyLimit > len(items) {
		historyLimit = len(items)
	}

	if historyLimit > 0 {
		items = items[:historyLimit]
	}

	close(done) // spinner durdur
	ui.ClearScreen()

	if len(items) == 0 {
		err = fmt.Errorf("Bu kaynak için geçmiş bulunamadı")
		fmt.Printf("\033[31m[!] %s\033[0m\n", err.Error())
		time.Sleep(1500 * time.Millisecond)
		return
	}

	// sadece key stringlerini çıkar
	var keys []string
	for _, it := range items {
		keys = append(keys, it.Key)
	}

	// TUI ile seçim al
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

	// seçilen animeyi bul
	found := false
	for _, it := range items {
		if it.Key == selectedKey {
			selectedAnime = it.AnimeName
			animeId = it.AnimeId
			lastEpisodeIdx = it.Idx
			found = true
			break
		}
	}
	if !found {
		err = fmt.Errorf("Seçilen anime bulunamadı: %s", selectedKey)
	}

	return
}

// Kullanıcıdan arama girdisi alır ve API üzerinden sonuçları getirir
func SearchAnime(source models.AnimeSource, UiMode string, RofiFlags string, Logger *models.LogServ) ([]models.Anime, []string, []string, map[string]models.Anime, error) {
	for {
		// Kullanıcıdan arama kelimesi al
		query, err := ui.InputFromUser(internal.UiParams{Mode: UiMode, RofiFlags: &RofiFlags, Label: "Anime ara "})

		if errors.Is(err, tui.ErrGoBack) {
			// kullanıcı ESC bastı → fonksiyonu çağıran yere geri dön
			return nil, nil, nil, nil, err
		}

		utils.FailIfErr(internal.UiParams{
			Mode:      UiMode,
			RofiFlags: &RofiFlags,
		}, err, Logger)

		// Loading spinner başlat
		done := make(chan struct{})
		go ui.ShowLoading(internal.UiParams{
			Mode:      UiMode,
			RofiFlags: &RofiFlags,
		}, "Aranıyor...", done)

		// API üzerinden arama yap
		searchData, err := source.GetSearchData(query)
		if err != nil {
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
		// Hiç sonuç çıkmazsa kullanıcıyı bilgilendir
		if searchData == nil {
			close(done)      // spinneri durdur
			ui.ClearScreen() // ekranı temizle
			fmt.Printf("\033[31m[!] Arama sonucu bulunamadı!\033[0m")
			time.Sleep(1500 * time.Millisecond)
			continue
		}

		// Arama sonuçlarını işleyip ilgili listeleri oluştur
		animeNames := make([]string, 0, len(searchData))
		animeTypes := make([]string, 0, len(searchData))
		animeMap := make(map[string]models.Anime)

		for _, item := range searchData {
			animeNames = append(animeNames, item.Title)
			animeMap[item.Title] = item

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
