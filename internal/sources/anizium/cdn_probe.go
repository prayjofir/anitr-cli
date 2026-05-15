package anizium

// cdn_probe.go
//
// Bu dosya, CDN sunucularına HEAD isteği atarak çalışan video URL'si bulma
// sistemini içerir. Anizium'un authenticated API'si kullanılmaya başlandıktan
// sonra bu kod GetWatchData'da aktif olarak kullanılmamaktadır.
//
// Hesap gerektirmeyen bir proje için (örneğin anonim izleyici CLI'ı) bu
// pattern'i kullanabilirsiniz:
//   URL: https://<server>/<tmdb_slug>/<season>/<episode>/<quality>-<sound>/master.m3u8
//   Örnek: https://x.aniziumserver.sbs/95479/1/1/1080p-original/master.m3u8
//
// m3u8 formatı yerine mp4 formatı da var (API'den gelen URL'lerde görüldüğü gibi):
//   URL: https://<server>/<tmdb_slug>/<season>/<episode>/<quality>.<sound>.mp4
//   Örnek: https://x.aniziumserver.site/95479/1/1/1080p.original.mp4
//
// Ses kodları: original (Japonca), trdub (Türkçe Dublaj), endub (İngilizce Dublaj)
// Kaliteler:   480p, 720p, 1080p, 1440p (2K), 2160p (4K)

import (
	"fmt"
	"net/http"
	"time"
)

// cdnServers, Anizium'un video CDN sunucu listesidir.
// Prefixler: a, f, k, r, u, x — her biri hem .sbs hem .site domain'inde.
var cdnServers = []string{
	"https://a.aniziumserver.sbs",
	"https://a.aniziumserver.site",
	"https://f.aniziumserver.sbs",
	"https://f.aniziumserver.site",
	"https://k.aniziumserver.sbs",
	"https://k.aniziumserver.site",
	"https://r.aniziumserver.sbs",
	"https://r.aniziumserver.site",
	"https://u.aniziumserver.sbs",
	"https://u.aniziumserver.site",
	"https://x.aniziumserver.sbs",
	"https://x.aniziumserver.site",
}

// cdnQualityList, yüksekten düşüğe CDN kalite sırası.
var cdnQualityList = []string{"2160p", "1440p", "1080p", "720p", "480p"}

// cdnSoundGroups, varsayılan ses grubu listesi.
var cdnSoundGroups = []string{"original", "trdub", "endub", "trsub"}

// ProbeCDNForURL, verilen tmdb slug, sezon, bölüm, kalite ve ses için
// çalışan bir CDN URL'si bulur. Hesap gerektirmez.
// URL formatı: https://<server>/<tmdb>/<season>/<episode>/<quality>-<sound>/master.m3u8
func ProbeCDNForURL(tmdbSlug string, seasonNum, episodeNum int, quality, sound string, isMovie bool) string {
	client := &http.Client{Timeout: 4 * time.Second}
	for _, srv := range cdnServers {
		var streamURL string
		if isMovie {
			streamURL = fmt.Sprintf("%s/%s/%s-%s/master.m3u8", srv, tmdbSlug, quality, sound)
		} else {
			streamURL = fmt.Sprintf("%s/%s/%d/%d/%s-%s/master.m3u8",
				srv, tmdbSlug, seasonNum, episodeNum, quality, sound)
		}
		req, err := http.NewRequest("HEAD", streamURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == 200 {
			return streamURL
		}
	}
	return ""
}

// ProbeCDNAll, tüm kalite ve ses kombinasyonlarını deneyerek
// çalışan URL'leri döner. Hesap gerektirmez.
// Yüksek kaliteden düşüğe doğru tarar.
func ProbeCDNAll(tmdbSlug string, seasonNum, episodeNum int, isMovie bool) []AnimeVideoSource {
	var results []AnimeVideoSource
	for _, qual := range cdnQualityList {
		for _, sound := range cdnSoundGroups {
			url := ProbeCDNForURL(tmdbSlug, seasonNum, episodeNum, qual, sound, isMovie)
			if url != "" {
				qualLabel := qual
				if qual == "2160p" {
					qualLabel = "4K"
				} else if qual == "1440p" {
					qualLabel = "2K"
				}
				results = append(results, AnimeVideoSource{
					Label:   buildVideoLabel(qualLabel, sound),
					URL:     url,
					Quality: qualLabel,
					Sound:   sound,
				})
			}
		}
	}
	return results
}
