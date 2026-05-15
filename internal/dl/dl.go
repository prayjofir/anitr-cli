package dl

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/prayjofir/anitr-cli/internal/config"
	"github.com/prayjofir/anitr-cli/internal/utils"
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

	err = os.MkdirAll(baseDir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDirCreate, err)
	}

	return &Downloader{BinPath: bin, BaseDir: baseDir}, nil
}

// Download -> anime adı + bölüm + url alır, dosyayı indirir.
// subtitlePath boşsa altyazı eklenmez; doluysa video yanına .vtt olarak kopyalanır.
func (d *Downloader) Download(source, animeName, url string, episodeNumber float64, seasonNumber int, subtitlePath string) error {
	// Çıkış klasörü
	outDir := filepath.Join(d.BaseDir, source, animeName)
	err := os.MkdirAll(outDir, 0o755)
	if err != nil {
		return fmt.Errorf("klasör oluşturulamadı: %w", err)
	}

	// Bölüm numarasını düzgün formatla
	var epStr string
	if episodeNumber == float64(int(episodeNumber)) {
		epStr = fmt.Sprintf("E%02d", int(episodeNumber))
	} else {
		epStr = fmt.Sprintf("E%.1f", episodeNumber)
	}

	// Dosya adı
	outFile := filepath.Join(outDir, fmt.Sprintf("S%02d%s.%%(ext)s", seasonNumber, epStr))

	// Video indir
	cmd := exec.Command(d.BinPath, "-o", outFile, url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	// Altyazı varsa video yanına kopyala (MPV/VLC otomatik tanır)
	if subtitlePath != "" {
		subDest := filepath.Join(outDir, fmt.Sprintf("S%02d%s.vtt", seasonNumber, epStr))
		if copyErr := copyFile(subtitlePath, subDest); copyErr != nil {
			fmt.Printf("\033[33m⚠️  Altyazı kopyalanamadı: %s\033[0m\n", copyErr)
		} else {
			fmt.Printf("\033[32m   Altyazı kaydedildi: %s\033[0m\n", filepath.Base(subDest))
		}
	}

	return nil
}

// copyFile, src dosyasını dst'ye kopyalar.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
