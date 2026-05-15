package aniziumfree

// subtitle_discovery.go
//
// # NASIL ÇALIŞIR?
//
// Anizium altyazıları şu URL formatıyla sunulur:
//   https://{sunucu}/api/subtitle/get/file.vtt?id={animeID}&name={isim}&season={s}&episode={e}&type=vtt
//
// Sunucu prefixleri video CDN ile aynı deseni izler: a, f, k, r, u, x
// Bu sunucular üç domain üzerinde çalışabilir:
//   - {prefix}.anizium.co  (bilinen aktif)
//   - {prefix}.anizium.sbs (deneme adayı)
//   - {prefix}.anizium.site (deneme adayı)
//
// Keşif mekanizması — iki aşamalı:
//
// 1) BAŞLANGIÇ PROBU (uygulama açılışında, arka planda):
//    - Disk cache'den son bilinen bir VTT URL'si okunur.
//    - Bu URL'nin sunucu prefixini değiştirerek tüm adaylar paralel HEAD ile denenir.
//    - 200 dönen sunucular "aktif liste"ye eklenir ve cache güncellenir.
//    - Disk cache yoksa sadece varsayılan sunucu (x.anizium.co) aktif olarak işaretlenir.
//
// 2) LAZY DISCOVERY (ilk altyazı çekildiğinde):
//    - API'den gelen gerçek bir VTT URL'si ile tüm adaylar paralel HEAD ile denenir.
//    - 200 dönen sunucular aktif listeye alınır.
//    - Sonuç diske kaydedilir → bir sonraki başlangıç probu bu URL'yi kullanır.
//    - En hızlı aktif sunucu MPV'ye verilen URL'ye uygulanır (ReplaceSubtitleServer).
//
// NOT: Dil keşfi yapılmaz. Dil bilgisi URL'deki "name" parametresine gömülüdür ve
//      sunucu değiştirerek tahmin edilemez. Farklı dil altyazıları yalnızca
//      kimlik doğrulamalı /anime/source çağrısıyla alınabilir.
//
// Cache dosyası: ~/.config/anitr-cli/subtitle_servers.json
// Format: {"last_vtt_url":"...","active_servers":["https://x.anizium.co",...],"updated_at":"..."}

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/axrona/anitr-cli/internal/helpers"
)

// ── Sunucu adayları ────────────────────────────────────────────────────────────

// Bilinen aktif sunucu prefix'leri (hızlı probe için önce bunlar denenir)
var knownActivePrefixes = []string{"a", "f", "k", "r", "u", "x"}

// Tüm olası prefix adayları: a-z + 0-9 (yeni sunucu keşfi için)
var allPrefixCandidates = func() []string {
	var all []string
	// Bilinen aktifler önce
	all = append(all, knownActivePrefixes...)
	// Geri kalan harfler ve sayılar
	for c := 'a'; c <= 'z'; c++ {
		p := string(c)
		known := false
		for _, k := range knownActivePrefixes {
			if k == p {
				known = true
				break
			}
		}
		if !known {
			all = append(all, p)
		}
	}
	return all
}()

// Denenecek domain suffix'leri
var subtitleDomainSuffixes = []string{
	"anizium.co",
	"anizium.sbs",
	"anizium.site",
}

// buildCandidateServers — prefix × suffix matrisinden aday sunucu base URL'lerini üretir.
// fullScan=true → tüm a-z taranır, false → sadece bilinen aktif prefixler
func buildCandidateServers(fullScan bool) []string {
	prefixes := knownActivePrefixes
	if fullScan {
		prefixes = allPrefixCandidates
	}
	var servers []string
	for _, prefix := range prefixes {
		for _, suffix := range subtitleDomainSuffixes {
			servers = append(servers, fmt.Sprintf("https://%s.%s", prefix, suffix))
		}
	}
	return servers
}

// buildAllCandidateServers — geriye dönük uyumluluk için (tam tarama)
func buildAllCandidateServers() []string {
	return buildCandidateServers(true)
}

// ── Disk cache ─────────────────────────────────────────────────────────────────

type subtitleCache struct {
	LastVTTURL    string   `json:"last_vtt_url"`
	ActiveServers []string `json:"active_servers"`
	UpdatedAt     string   `json:"updated_at"`
}

func subtitleCachePath() string {
	return filepath.Join(helpers.ConfigDir(), "subtitle_servers.json")
}

func loadSubtitleCache() *subtitleCache {
	data, err := os.ReadFile(subtitleCachePath())
	if err != nil {
		return nil
	}
	var c subtitleCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	return &c
}

func saveSubtitleCache(c *subtitleCache) {
	c.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	path := subtitleCachePath()
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, data, 0644)
}

// ── Aktif sunucu listesi (oturum içi bellek) ───────────────────────────────────

var (
	activeServers        []string
	activeServersMu      sync.RWMutex
	discoveryOnce        sync.Once // StartSubtitleDiscovery oturum içinde yalnızca bir kez çalışır
)

// GetActiveServers — aktif altyazı sunucularını döner (thread-safe)
func GetActiveServers() []string {
	activeServersMu.RLock()
	defer activeServersMu.RUnlock()
	if len(activeServers) == 0 {
		return []string{"https://x.anizium.co"}
	}
	return append([]string{}, activeServers...)
}

func setActiveServers(servers []string) {
	activeServersMu.Lock()
	defer activeServersMu.Unlock()
	activeServers = servers
}

// ── Probe fonksiyonları ────────────────────────────────────────────────────────

// probeServerWithURL — verilen sunucu base URL'si ile tam VTT URL'sini dener (HEAD isteği).
// 200 dönerse true, aksi halde false.
func probeServerWithURL(serverBase, originalURL string) bool {
	parsed, err := url.Parse(originalURL)
	if err != nil {
		return false
	}
	candidateParsed, err := url.Parse(serverBase)
	if err != nil {
		return false
	}
	parsed.Scheme = candidateParsed.Scheme
	parsed.Host = candidateParsed.Host

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("HEAD", parsed.String(), nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// probeWithList — verilen sunucu listesini paralel HEAD ile dener, 200 dönen base URL'leri döner.
func probeWithList(candidates []string, knownVTTURL string) []string {
	resultCh := make(chan string, len(candidates))
	var wg sync.WaitGroup

	for _, srv := range candidates {
		wg.Add(1)
		go func(serverBase string) {
			defer wg.Done()
			if probeServerWithURL(serverBase, knownVTTURL) {
				resultCh <- serverBase
			}
		}(srv)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var working []string
	for srv := range resultCh {
		working = append(working, srv)
	}
	return working
}

// probeAndCollect — tüm aday sunucuları (a-z × domain) dener. Lazy discovery için kullanılır.
func probeAndCollect(knownVTTURL string) []string {
	return probeWithList(buildAllCandidateServers(), knownVTTURL)
}

// ── Başlangıç probu (uygulama açılışında arka planda) ─────────────────────────

// StartSubtitleDiscovery — uygulama açılışında arka planda çalıştırılır.
// sync.Once ile oturum içinde yalnızca bir kez çalışır — menüye her dönüşte tekrar başlamaz.
//
// Mantık:
//   - Cache'de aktif sunucular varsa → anında belleğe al (0ms), direkt kullan
//   - Cache yoksa veya boşsa → direkt geç, lazy discovery bekle
//   - Her iki durumda da arka planda tam a-z taraması → yeni sunucu keşfi + cache güncelleme
func StartSubtitleDiscovery() {
	discoveryOnce.Do(func() {
		go func() {
			cache := loadSubtitleCache()

			// Cache'de aktif sunucular varsa direkt kullan
			if cache != nil && len(cache.ActiveServers) > 0 {
				setActiveServers(cache.ActiveServers)
			} else {
				setActiveServers([]string{"https://x.anizium.co"})
			}

			// Cache'de VTT URL yoksa tam tarama yapacak referans yok — lazy discovery bekle
			if cache == nil || cache.LastVTTURL == "" {
				return
			}

			// Arka planda tam a-z taraması: yeni sunucu keşfeder → cache güncellenir
			// Bir sonraki açılışta yeni sunucu da cache'den direkt yüklenir
			go func() {
				allWorking := probeWithList(buildCandidateServers(true), cache.LastVTTURL)
				if len(allWorking) > 0 {
					setActiveServers(allWorking)
					cache.ActiveServers = allWorking
					saveSubtitleCache(cache)
				}
			}()
		}()
	})
}

// ── Lazy discovery (ilk altyazı çekildiğinde) ─────────────────────────────────

// DiscoveredSubtitle — sunucu probu sonucu bulunan bir altyazı
type DiscoveredSubtitle struct {
	Group string // "tr", "en", "ja", ...
	URL   string // Çalışan VTT URL'si
	Label string // "Türkçe", "İngilizce", ...
}

// DiscoverAndDownloadSubtitles — verilen altyazı URL'sinden hareketle:
// 1) Tüm sunucu adaylarını paralel HEAD ile dener (200 alanlar → aktif liste güncellenir + cache)
// 2) Bilinen URL'yi en hızlı aktif sunucuya yönlendirerek döner
//
// NOT: Dil keşfi yapılmaz — dil bilgisi URL'deki "name" parametresine gömülüdür.
func DiscoverAndDownloadSubtitles(knownURL, knownGroup string) []DiscoveredSubtitle {
	if knownURL == "" {
		return nil
	}

	// Tüm sunucu adaylarını paralel dene → aktif olanları güncelle
	working := probeAndCollect(knownURL)
	if len(working) > 0 {
		setActiveServers(working)
		cache := loadSubtitleCache()
		if cache == nil {
			cache = &subtitleCache{}
		}
		cache.LastVTTURL = knownURL
		cache.ActiveServers = working
		saveSubtitleCache(cache)
	}

	// Bilinen URL'yi en hızlı aktif sunucudan servis et
	bestURL := ReplaceSubtitleServer(knownURL)
	return []DiscoveredSubtitle{{
		Group: knownGroup,
		URL:   bestURL,
		Label: subtitleLabel(knownGroup),
	}}
}

// ── Yardımcı ──────────────────────────────────────────────────────────────────

func subtitleLabel(group string) string {
	labels := map[string]string{
		"tr": "Türkçe", "en": "İngilizce", "ja": "Japonca",
		"de": "Almanca", "fr": "Fransızca", "es": "İspanyolca",
		"ar": "Arapça", "pt": "Portekizce", "it": "İtalyanca",
	}
	if l, ok := labels[strings.ToLower(group)]; ok {
		return l
	}
	return strings.ToUpper(group)
}

// ReplaceSubtitleServer — mevcut bir VTT URL'sinin sunucusunu aktif listeden birincisi ile değiştirir.
// MPV'ye vermeden önce URL'nin aktif sunucudan gelmesini sağlar.
func ReplaceSubtitleServer(originalURL string) string {
	servers := GetActiveServers()
	if len(servers) == 0 {
		return originalURL
	}
	parsed, err := url.Parse(originalURL)
	if err != nil {
		return originalURL
	}
	serverParsed, err := url.Parse(servers[0])
	if err != nil {
		return originalURL
	}
	parsed.Host = serverParsed.Host
	parsed.Scheme = serverParsed.Scheme
	return parsed.String()
}
