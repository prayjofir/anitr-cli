package player

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/axrona/anitr-cli/internal/ipc"
)

// PlayWithPlaylist, birden fazla bölümü playlist olarak başlatır.
func PlayWithPlaylist(params []MPVParams) (*exec.Cmd, string, error) {
	if len(params) == 0 {
		return nil, "", errors.New("playlist boş olamaz")
	}

	mpvSocketPath := getMPVSocketPath()

	// MPV'nin yüklü olup olmadığını kontrol et
	if err := isMPVInstalled(); err != nil {
		return nil, "", errors.New("mpv sisteminizde yüklü değil")
	}

	// İlk bölümü başlatmak için MPV komutunu oluştur
	args := []string{
		"--fullscreen", // Tam ekran başlat
		"--save-position-on-quit",
		fmt.Sprintf("--title=%s", params[0].Title),
		fmt.Sprintf("--force-media-title=%s", params[0].Title),
		"--idle=once", "--really-quiet", "--no-terminal",
		fmt.Sprintf("--input-ipc-server=%s", mpvSocketPath),
		"--playlist-start=0", // Playlist'i baştan başlat
	}

	// Platform bazlı user-agent ve referrer ayarı
	if runtime.GOOS == "linux" {
		args = append(args,
			"--user-agent=Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/137.0.0.0 Safari/537.36",
			"--referrer=https://yeshi.eu.org/")
	} else if runtime.GOOS == "windows" {
		args = append(args,
			"--user-agent=Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
			"--referrer=https://yeshi.eu.org/")
	}

	// İlk bölümün altyazısını ekle
	if params[0].SubtitleUrl != nil && *params[0].SubtitleUrl != "" {
		args = append(args, fmt.Sprintf("--sub-file=%s", *params[0].SubtitleUrl))
	}

	// İlk bölümün URL'sini ekle
	args = append(args, params[0].Url)

	mpvBinary := getMPVBinary()
	cmd := exec.Command(mpvBinary, args...)
	if err := cmd.Start(); err != nil {
		return cmd, "", err
	}

	// MPV'nin IPC soketinin hazır olmasını bekle
	maxRetries := 25
	retryDelay := 300 * time.Millisecond
	for i := 0; i < maxRetries; i++ {
		time.Sleep(retryDelay)
		conn, err := ipc.ConnectToPipe(mpvSocketPath)
		if err == nil {
			conn.Close()
			
			// Kalan bölümleri playlist'e ekle
			for i := 1; i < len(params); i++ {
				playlistArgs := []interface{}{"loadfile", params[i].Url, "append-play"}
				_, err := MPVSendCommand(mpvSocketPath, playlistArgs)
				if err != nil {
					// Hata durumunda devam et, sadece logla
					continue
				}
				
				// Altyazıyı ekle (varsa)
				if params[i].SubtitleUrl != nil && *params[i].SubtitleUrl != "" {
					subArgs := []interface{}{"sub-add", *params[i].SubtitleUrl}
					MPVSendCommand(mpvSocketPath, subArgs)
				}
			}
			
			return cmd, mpvSocketPath, nil
		}
	}

	return cmd, "", errors.New("MPV socket hazır değil, başlatılamadı")
}

// GetCurrentPlaylistPos, MPV'nin şu anki playlist pozisyonunu döner.
func GetCurrentPlaylistPos(socketPath string) (int, error) {
	pos, err := MPVSendCommand(socketPath, []interface{}{"get_property", "playlist-pos"})
	if err != nil {
		return -1, err
	}
	
	if pos == nil {
		return -1, nil
	}
	
	position, ok := pos.(float64)
	if !ok {
		return -1, fmt.Errorf("playlist pozisyonu beklenen formatta değil")
	}
	
	return int(position), nil
}

// GetPlaylistCount, playlist'teki toplam öğe sayısını döner.
func GetPlaylistCount(socketPath string) (int, error) {
	count, err := MPVSendCommand(socketPath, []interface{}{"get_property", "playlist-count"})
	if err != nil {
		return -1, err
	}
	
	if count == nil {
		return -1, nil
	}
	
	playlistCount, ok := count.(float64)
	if !ok {
		return -1, fmt.Errorf("playlist sayısı beklenen formatta değil")
	}
	
	return int(playlistCount), nil
}

// UpdateMPVTitle, MPV'nin başlığını günceller
func UpdateMPVTitle(socketPath, title string) error {
	// MPV'de title güncellemek için force-media-title property'sini kullan
	_, err := MPVSendCommand(socketPath, []interface{}{"set_property", "force-media-title", title})
	if err != nil {
		// Alternatif olarak title property'sini dene
		_, err = MPVSendCommand(socketPath, []interface{}{"set_property", "title", title})
	}
	return err
}
