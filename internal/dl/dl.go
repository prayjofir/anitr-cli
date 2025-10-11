package dl

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/axrona/anitr-cli/internal/config"
	"github.com/axrona/anitr-cli/internal/utils"
)

// Sentinel error tipleri
var (
	ErrNoDownloader = errors.New("youtube-dl veya yt-dlp bulunamadı")
	ErrDirCreate    = errors.New("klasör oluşturulamadı")
)

// Downloader struct
type Downloader struct {
	BinPath string
	BaseDir string
}

func VideosDir() string {
	cfg, err := config.LoadConfig(filepath.Join(utils.ConfigDir(), "config.json"))
	if err == nil && cfg.DownloadDir != "" {
		return cfg.DownloadDir
	}

	// fallback → eski davranış
	if runtime.GOOS == "windows" {
		userProfile := os.Getenv("USERPROFILE")
		if userProfile == "" {
			userProfile = "."
		}
		return filepath.Join(userProfile, "Videos")
	} else {
		home := os.Getenv("HOME")
		if home == "" {
			home = "."
		}
		return filepath.Join(home, "Videos")
	}
}

// NewDownloader -> Downloader oluşturur, gerekli binary ve klasörleri kontrol eder
func NewDownloader(baseDir string) (*Downloader, error) {
	bin, err := exec.LookPath("yt-dlp")
	if err != nil {
		bin, err = exec.LookPath("youtube-dl")
		if err != nil {
			return nil, ErrNoDownloader
		}
	}

	// Klasörü oluştur
	err = os.MkdirAll(baseDir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDirCreate, err)
	}

	return &Downloader{BinPath: bin, BaseDir: baseDir}, nil
}

// Download -> anime adı + bölüm + url alır, dosyayı indirir
func (d *Downloader) Download(source, animeName, url string, episodeNumber float64, seasonNumber int) error {
	// Çıkış klasörü
	outDir := filepath.Join(d.BaseDir, source, animeName)
	err := os.MkdirAll(outDir, 0o755)
	if err != nil {
		return fmt.Errorf("klasör oluşturulamadı: %w", err)
	}

	// Bölüm numarasını düzgün formatla
	var epStr string
	if episodeNumber == float64(int(episodeNumber)) {
		// Tam sayı bölüm (ör. 12.0 -> 12)
		epStr = fmt.Sprintf("E%02d", int(episodeNumber))
	} else {
		// Ara bölüm (ör. 7.5 -> E07.5)
		epStr = fmt.Sprintf("E%.1f", episodeNumber)
	}

	// Dosya adı
	outFile := filepath.Join(outDir, fmt.Sprintf("S%02d%s.%%(ext)s", seasonNumber, epStr))

	// Komutu çalıştır
	cmd := exec.Command(d.BinPath, "-o", outFile, url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
