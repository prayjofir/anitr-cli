package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/prayjofir/anitr-cli/internal/actions"
	"github.com/prayjofir/anitr-cli/internal/cli"
	"github.com/prayjofir/anitr-cli/internal/config"
	"github.com/prayjofir/anitr-cli/internal/flags"
	"github.com/prayjofir/anitr-cli/internal/helpers"
	"github.com/prayjofir/anitr-cli/internal/history"
	"github.com/prayjofir/anitr-cli/internal/models"
	"github.com/prayjofir/anitr-cli/internal/sources/animecix"
	"github.com/prayjofir/anitr-cli/internal/sources/anizium"
	"github.com/prayjofir/anitr-cli/internal/sources/aniziumfree"
	"github.com/prayjofir/anitr-cli/internal/sources/openanime"
	"github.com/prayjofir/anitr-cli/internal/update"
	"github.com/prayjofir/anitr-cli/internal/utils"
	"github.com/spf13/cobra"
)

// Ana uygulama döngüsünü yöneten fonksiyon
func runMain(cmd *cobra.Command, f *flags.Flags, UiMode string, Logger *models.LogServ) {
	// RPC'yi devre dışı bırakma bayrağı ayarlanır
	DisableRPC := f.DisableRPC

	// Güncellemeleri kontrol et
	update.CheckUpdates()

	// Geçmişi yükle
	AnimeHistory, err := history.ReadAnimeHistory()
	if err != nil {
		utils.LogError(Logger, fmt.Errorf(fmt.Sprintf("Geçmiş yüklenemedi: %s", err)))
	}

	// Uygulama durumunu başlat — varsayılan: AnimeciX
	currentApp := &models.App{
		Source:         helpers.Ptr(models.AnimeSource(animecix.AnimeCix{})),
		SelectedSource: helpers.Ptr("AnimeciX"),
		UiMode:         &UiMode,
		RofiFlags:      &f.RofiFlags,
		DisableRPC:     &DisableRPC,
		AnimeHistory:   &AnimeHistory,
		HistoryLimit:   0,
		Logger:         Logger,
	}

	// Configi yükle
	configPath := filepath.Join(utils.ConfigDir(), "config.json")
	cfg, err := config.LoadConfig(configPath)
	if err == nil {
		// Son kullanılan kaynağı yükle
		switch strings.ToLower(cfg.LastSource) {
		case "openanime":
			currentApp.Source = helpers.Ptr(models.AnimeSource(openanime.OpenAnime{}))
			currentApp.SelectedSource = helpers.Ptr("OpenAnime")
		case "animecix":
			currentApp.Source = helpers.Ptr(models.AnimeSource(animecix.AnimeCix{}))
			currentApp.SelectedSource = helpers.Ptr("AnimeciX")
		case "anizium":
			currentApp.Source = helpers.Ptr(models.AnimeSource(anizium.Anizium{}))
			currentApp.SelectedSource = helpers.Ptr("Anizium")
		case "anizium free":
			currentApp.Source = helpers.Ptr(models.AnimeSource(aniziumfree.AniziumFree{}))
			currentApp.SelectedSource = helpers.Ptr("Anizium Free")
		// Boşsa veya tanınmıyorsa AnimeciX kalır (üstte set edildi)
		}

		if cfg.DisableRPC != nil {
			currentApp.DisableRPC = cfg.DisableRPC
		}
		currentApp.HistoryLimit = cfg.HistoryLimit
	}

	if cmd.Flags().Changed("disable-rpc") {
		currentApp.DisableRPC = &DisableRPC
	}

	timestamp := time.Now()

	// --go bayrağı kontrol edilir
	if f.QuickResume {
		err := actions.QuickResumeLastAnime(currentApp, timestamp)
		if err != nil {
			utils.LogError(Logger, fmt.Errorf("hızlı devam etme hatası: %w", err))
			// Hata durumunda normal menüye geç
		} else {
			// Başarılı olursa uygulamayı kapat
			return
		}
	}

	for {
		cli.MainMenu(currentApp, timestamp)
	}
}

// Uygulama komutlarını çalıştıran giriş fonksiyonu
func RunApp() {
	Logger, err := utils.NewLogger()
	if err != nil {
		panic(err)
	}
	defer utils.Close(Logger)
	log.SetFlags(0)

	rootCmd, f := flags.NewFlagsCmd()

	commands := rootCmd.Commands()

	if runtime.GOOS != "linux" {
		// Windows ve Mac'te alt komut yok, doğrudan tui modunda çalıştır
		rootCmd.Run = func(cmd *cobra.Command, args []string) {
			f.RofiMode = false
			runMain(rootCmd, f, "tui", Logger)
		}
	} else {
		// Linux için alt komutlar varsa ayarla
		var rofiCmd, tuiCmd *cobra.Command
		if len(commands) > 0 {
			rofiCmd = commands[0]
		}
		if len(commands) > 1 {
			tuiCmd = commands[1]
		}

		if rofiCmd != nil {
			rofiCmd.Run = func(cmd *cobra.Command, args []string) {
				f.RofiMode = true
				runMain(rootCmd, f, "rofi", Logger)
			}
		}

		if tuiCmd != nil {
			tuiCmd.Run = func(cmd *cobra.Command, args []string) {
				f.RofiMode = false
				runMain(rootCmd, f, "tui", Logger)
			}
		}

		rootCmd.Run = func(cmd *cobra.Command, args []string) {
			f.RofiMode = false
			runMain(rootCmd, f, "tui", Logger)
		}
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
