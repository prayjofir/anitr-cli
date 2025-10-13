package player

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/axrona/anitr-cli/internal/ipc"
)

// createLuaScript, başlık güncellemesi için Lua script oluşturur
func createLuaScript(params []MPVParams, startIndex int) (string, error) {
	tempDir := os.TempDir()
	scriptPath := filepath.Join(tempDir, "anitr-title-updater.lua")
	
	// startIndex'i sınırla
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex >= len(params) {
		startIndex = len(params) - 1
	}
	
	// Lua script içeriği
	script := `-- Anitr-CLI automatic title updater
local titles = {
`
	
	// Her bölüm için başlık bilgisini ekle
	for i, param := range params {
		// Lua string'inde özel karakterleri escape et
		escapedTitle := param.Title
		escapedTitle = strings.ReplaceAll(escapedTitle, "\\", "\\\\")
		escapedTitle = strings.ReplaceAll(escapedTitle, "\"", "\\\"")
		script += fmt.Sprintf("    [%d] = \"%s\",\n", i, escapedTitle)
	}
	
	// Başlangıç pozisyonunu ekle
	script += fmt.Sprintf("}\nlocal start_index = %d\n\n", startIndex)
	
	script += `-- Playlist pozisyonu değiştiğinde başlığı güncelle
mp.observe_property("playlist-pos", "number", function(name, value)
    if value ~= nil and titles[value] ~= nil then
        local title = titles[value]
        mp.set_property("force-media-title", title)
        mp.osd_message(title, 3)
    end
end)

-- İlk başlık (start_index'ten başla)
if titles[start_index] ~= nil then
    mp.set_property("force-media-title", titles[start_index])
end
`
	
	// Script dosyasını yaz
	err := os.WriteFile(scriptPath, []byte(script), 0644)
	if err != nil {
		return "", fmt.Errorf("lua script oluşturulamadı: %w", err)
	}
	
	return scriptPath, nil
}

// createM3U8Playlist, verilen parametrelerle M3U8 playlist dosyası oluşturur
func createM3U8Playlist(params []MPVParams) (string, error) {
	// Temp dizininde playlist dosyası oluştur
	tempDir := os.TempDir()
	playlistPath := filepath.Join(tempDir, "anitr-playlist.m3u8")
	
	// M3U8 dosyası oluştur
	f, err := os.Create(playlistPath)
	if err != nil {
		return "", fmt.Errorf("playlist dosyası oluşturulamadı: %w", err)
	}
	defer f.Close()
	
	// M3U8 header
	f.WriteString("#EXTM3U\n")
	
	// Her bölüm için entry ekle
	for _, param := range params {
		// Başlık bilgisini ekle
		f.WriteString(fmt.Sprintf("#EXTINF:-1,%s\n", param.Title))
		// URL
		f.WriteString(fmt.Sprintf("%s\n", param.Url))
	}
	
	return playlistPath, nil
}

// PlayWithPlaylist, birden fazla bölümü playlist olarak başlatır.
func PlayWithPlaylist(params []MPVParams, startIndex int) (*exec.Cmd, string, error) {
	if len(params) == 0 {
		return nil, "", errors.New("playlist boş olamaz")
	}

	// startIndex'i kontrol et ve sınırla
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex >= len(params) {
		startIndex = len(params) - 1
	}

	mpvSocketPath := getMPVSocketPath()

	// MPV'nin yüklü olup olmadığını kontrol et
	if err := isMPVInstalled(); err != nil {
		return nil, "", errors.New("mpv sisteminizde yüklü değil")
	}

	// Lua script oluştur (başlık güncellemeleri için, startIndex ile)
	luaScriptPath, err := createLuaScript(params, startIndex)
	if err != nil {
		return nil, "", err
	}

	// M3U8 playlist dosyası oluştur
	playlistPath, err := createM3U8Playlist(params)
	if err != nil {
		os.Remove(luaScriptPath)
		return nil, "", err
	}

	// MPV komutunu oluştur
	args := []string{
		"--fullscreen", // Tam ekran başlat
		"--save-position-on-quit",
		"--idle=once", "--no-terminal",
		fmt.Sprintf("--input-ipc-server=%s", mpvSocketPath),
		fmt.Sprintf("--script=%s", luaScriptPath), // Lua script'i yükle
		fmt.Sprintf("--playlist-start=%d", startIndex), // Başlangıç pozisyonu
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

	// Playlist dosyasını ekle
	args = append(args, fmt.Sprintf("--playlist=%s", playlistPath))

	mpvBinary := getMPVBinary()
	cmd := exec.Command(mpvBinary, args...)
	if err := cmd.Start(); err != nil {
		os.Remove(playlistPath)
		os.Remove(luaScriptPath)
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
			// MPV başladı, biraz bekle sonra dosyaları sil
			time.Sleep(1 * time.Second)
			os.Remove(playlistPath)
			os.Remove(luaScriptPath)
			return cmd, mpvSocketPath, nil
		}
	}

	// Başarısız olursa temizlik yap
	os.Remove(playlistPath)
	os.Remove(luaScriptPath)
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
	var lastErr error
	
	// 1. force-media-title'ı ayarla (pencere başlığı için)
	_, err := MPVSendCommand(socketPath, []interface{}{"set_property", "force-media-title", title})
	if err != nil {
		lastErr = err
	}
	
	// 2. media-title'ı da ayarlamayı dene (bazı durumlarda bu da gerekebilir)
	_, err = MPVSendCommand(socketPath, []interface{}{"set_property", "media-title", title})
	if err != nil && lastErr == nil {
		lastErr = err
	}
	
	// 3. OSD mesajını göster (görsel feedback)
	MPVSendCommand(socketPath, []interface{}{"show-text", title, 2000})
	
	return lastErr
}

// PlaylistChangeCallback, playlist pozisyonu değiştiğinde çağrılacak fonksiyon tipi
type PlaylistChangeCallback func(position int, episodeName string)

// TrackPlaylistWithEvents, MPV event'lerini dinleyerek playlist takibi yapar
func TrackPlaylistWithEvents(socketPath, animeName string, episodeNames []string, startIndex int, onPositionChange PlaylistChangeCallback) {
	conn, err := ipc.ConnectToPipe(socketPath)
	if err != nil {
		return
	}
	defer conn.Close()
	
	// Başlangıç pozisyonunu kontrol et
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex >= len(episodeNames) {
		startIndex = len(episodeNames) - 1
	}
	
	// playlist-pos property'sini observe et (BU BAĞLANTIDA!)
	observeCmd := map[string]interface{}{
		"command": []interface{}{"observe_property", 1, "playlist-pos"},
	}
	observeJSON, _ := json.Marshal(observeCmd)
	observeJSON = append(observeJSON, '\n')
	conn.Write(observeJSON)
	
	// lastPosition'ı startIndex olarak başlat (ilk callback zaten actions.go'da çağrıldı)
	var lastPosition = startIndex
	
	// Event loop - MPV'den gelen mesajları dinle
	for {
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			// Bağlantı koptu, MPV kapanmış olabilir
			return
		}
		
		if n == 0 {
			continue
		}
		
		// JSON yanıtını parse et
		var response map[string]interface{}
		if err := json.Unmarshal(buf[:n], &response); err != nil {
			continue
		}
		
		// Event kontrolü
		if event, ok := response["event"].(string); ok {
			// playlist-pos değişimi event'i
			if event == "property-change" {
				if name, ok := response["name"].(string); ok && name == "playlist-pos" {
					if data, ok := response["data"].(float64); ok {
						currentPos := int(data)
						
						// Pozisyon değiştiyse ve geçerliyse
						if currentPos != lastPosition && currentPos >= 0 && currentPos < len(episodeNames) {
							episodeName := episodeNames[currentPos]
							newTitle := fmt.Sprintf("%s - %s", animeName, episodeName)
							
							// Yeni bağlantı açıp başlığı güncelle (mevcut conn read modunda)
							go UpdateMPVTitle(socketPath, newTitle)
							
							// Callback'i çağır (history update vs.)
							if onPositionChange != nil {
								go onPositionChange(currentPos, episodeName)
							}
							
							lastPosition = currentPos
						}
					}
				}
			}
		}
	}
}
