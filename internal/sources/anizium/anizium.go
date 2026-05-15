package anizium

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/axrona/anitr-cli/internal"
	"github.com/axrona/anitr-cli/internal/config"
	"github.com/axrona/anitr-cli/internal/helpers"
	"github.com/axrona/anitr-cli/internal/models"
)

type Anizium struct{}

// getConfigPath — uygulamanın config.json yolunu döner
func getConfigPath() string {
	return filepath.Join(helpers.ConfigDir(), "config.json")
}

var configAnizium = internal.Config{
	BaseUrl:        "https://api.anizium.co/",
	AlternativeUrl: "https://anizium.com/",
	VideoPlayers:   []string{"f.aniziumserver.sbs"},
}

// xorEncrypt, JS tarafındaki şifrelemenin Go karşılığı
func xorEncrypt(text, key string) string {
	tb := []byte(text)
	kb := []byte(key)
	res := make([]byte, len(tb))
	for i, b := range tb {
		res[i] = b ^ kb[i%len(kb)]
	}
	return fmt.Sprintf("%x", res)
}

// randomString 6 haneli rastgele string üretir
func randomString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}

// generateCfToken - CF-Control header değerini üretir (auth.go tarafından da kullanılır)
func generateCfToken() string {
	tokenKey := "hlxjl1c2w281ax473rt1ofgrvhyjvi"
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	dayName := strings.ToLower(now.Format("Monday"))
	key := fmt.Sprintf("%s_%s", tokenKey, dayName)
	randStr := randomString(6)
	tMap := map[string]int64{randStr: now.UnixNano() / int64(time.Millisecond)}
	tBytes, _ := json.Marshal(tMap)
	return xorEncrypt(string(tBytes), key)
}

// getHeaders, Anizium API'sine istek yapmak için gerekli başlıkları dinamik üretir
func getHeaders() map[string]string {
	return map[string]string{
		"Accept":     "application/json",
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
		"Cf-Control": generateCfToken(),
		"device":     "browser",
		"language":   "tr",
		"site":       "main",
		"user-session": "",
		"user-profile": "",
	}
}

func (a Anizium) Source() string {
	return "Anizium"
}

func (a Anizium) GetSearchData(query string) ([]models.Anime, error) {
	searchUrl := fmt.Sprintf("%spage/search?value=%s&page=1", configAnizium.BaseUrl, url.QueryEscape(query))
	data, err := internal.GetJson(searchUrl, getHeaders())
	if err != nil {
		return nil, fmt.Errorf("anizium arama verisi alınamadı: %w", err)
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("yanıt beklenen formatta değil")
	}

	pageMap, ok := dataMap["page"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("'page' alanı bulunamadı")
	}

	itemsRaw, ok := pageMap["data"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("'data' alanı bulunamadı veya boş")
	}

	var returnData []models.Anime
	for _, item := range itemsRaw {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		title, _ := itemMap["name"].(string)
		slug, _ := itemMap["sef"].(string)
		poster, _ := itemMap["poster"].(string)
		animeType, _ := itemMap["type"].(string)
		
		// ID'yi string veya float64'ten int'e çeviriyoruz
		var id int
		if idStr, ok := itemMap["ID"].(string); ok {
			fmt.Sscanf(idStr, "%d", &id)
		} else if idFloat, ok := itemMap["ID"].(float64); ok {
			id = int(idFloat)
		}

		// TMDB ID'yi sakla (m3u8 linkinde bu kullanılıyor)
		var tmdbID int
		if tmdbStr, ok := itemMap["tmdb_id"].(string); ok {
			fmt.Sscanf(tmdbStr, "%d", &tmdbID)
		} else if tmdbFloat, ok := itemMap["tmdb_id"].(float64); ok {
			tmdbID = int(tmdbFloat)
		}

		extra := map[string]interface{}{
			"tmdb_id": tmdbID,
		}

		a := models.Anime{
			Title:     title,
			Slug:      &slug,
			ID:        &id,
			ImageURL:  poster,
			Type:      &animeType,
			TitleType: &animeType, // cli.go'nun isMovie tespiti için gerekli
			Extra:     extra,
		}

		returnData = append(returnData, a)
	}

	return returnData, nil
}

func (a Anizium) GetAnimeByID(id string) (*models.Anime, error) {
	urlStr := fmt.Sprintf("%sanime/get?id=%s", configAnizium.BaseUrl, id)
	data, err := internal.GetJson(urlStr, getHeaders())
	if err != nil {
		return nil, err
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("geçersiz yanıt")
	}
	itemMap, ok := dataMap["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("data alanı eksik")
	}

	title, _ := itemMap["name"].(string)
	return &models.Anime{Title: title}, nil
}

func (a Anizium) GetSeasonsData(params models.SeasonParams) ([]models.Season, error) {
	if params.Id == nil {
		return nil, fmt.Errorf("anime id gerekli")
	}

	urlStr := fmt.Sprintf("%sanime/get?id=%d", configAnizium.BaseUrl, *params.Id)
	data, err := internal.GetJson(urlStr, getHeaders())
	if err != nil {
		return nil, err
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("geçersiz format")
	}

	animeData, ok := dataMap["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("anime data eksik")
	}

	seasonsRaw, ok := animeData["seasons"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("seasons eksik")
	}

	var seasonList []models.Season
	var numbers []int

	for _, sRaw := range seasonsRaw {
		sMap, ok := sRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if numFloat, ok := sMap["number"].(float64); ok {
			numbers = append(numbers, int(numFloat))
		}
	}

	count := len(numbers)
	isMovie := false
	if animeData["type"] == "movie" {
		isMovie = true
	}

	seasonList = append(seasonList, models.Season{
		Seasons: &numbers,
		Count:   &count,
		IsMovie: &isMovie,
	})

	return seasonList, nil
}

func (a Anizium) GetEpisodesData(params models.EpisodeParams) ([]models.Episode, error) {
	if params.SeasonID == nil {
		return nil, fmt.Errorf("anime id gerekli")
	}

	// Anizium'da bölümler de aynı anime detayından geliyor. (Tek API call ile sezon/bölüm iniyor)
	urlStr := fmt.Sprintf("%sanime/get?id=%d", configAnizium.BaseUrl, *params.SeasonID)
	data, err := internal.GetJson(urlStr, getHeaders())
	if err != nil {
		return nil, err
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("geçersiz format")
	}

	animeData, ok := dataMap["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("anime data eksik")
	}

	var tmdbID int
	if tmdbStr, ok := animeData["tmdb_id"].(string); ok {
		fmt.Sscanf(tmdbStr, "%d", &tmdbID)
	} else if tmdbFloat, ok := animeData["tmdb_id"].(float64); ok {
		tmdbID = int(tmdbFloat)
	}

	seasonsRaw, ok := animeData["seasons"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("seasons eksik")
	}

	var epList []models.Episode
	// Şimdilik sadece tüm bölümleri listeliyoruz. Normalde secilen sezona göre filtrelemek gerekebilir.
	for _, sRaw := range seasonsRaw {
		sMap, ok := sRaw.(map[string]interface{})
		if !ok {
			continue
		}
		
		seasonNum := 1
		if sn, ok := sMap["number"].(float64); ok {
			seasonNum = int(sn)
		}

		episodesRaw, ok := sMap["episodes"].([]interface{})
		if !ok {
			continue
		}

		for _, eRaw := range episodesRaw {
			eMap, ok := eRaw.(map[string]interface{})
			if !ok {
				continue
			}

			title, _ := eMap["name"].(string)
			if title == "" {
				title = fmt.Sprintf("Bölüm")
			}
			var epNum int
			if n, ok := eMap["number"].(float64); ok {
				epNum = int(n)
			}
			
			// Video formatlaması için gerekli parametreleri extra alanına saklıyoruz.
			extra := map[string]interface{}{
				"season_num": float64(seasonNum),
				"episode_num": epNum,
				"tmdb_id": tmdbID,
				"quality": animeData["quality"], // "4k", "1080p" vb
			}

			ep := models.Episode{
				ID:     fmt.Sprintf("%v", eMap["ID"]),
				Title:  fmt.Sprintf("%d - %s", epNum, title),
				Number: epNum,
				Extra:  extra,
			}
			epList = append(epList, ep)
		}
	}

	return epList, nil
}

func (a Anizium) GetWatchData(params models.WatchParams) ([]models.Watch, error) {
	// utils.go tarafında extra içine seasonIndex ve episodeIndex atılmıştı.
	// Ama bölümleri oluştururken zaten Extra içerisine tmdb_id, season_num, episode_num gömdük.
	// Bölüm API'sini tekrar çağırmamıza gerek yok, eğer Extra üzerinden alabilirsek direk link üretiriz.
	// Fakat utils.go `episodeData[index].ID`yi url olarak gönderiyor. Oradan API çağırabiliriz veya 
	// biz TMDB id'sini animenin get endpointinden alabiliriz.

	if params.Id == nil {
		return nil, fmt.Errorf("anime id eksik")
	}

	// Tekrar anime detayını çekerek TMDB id'sine ve anime quality bilgisine erişiyoruz.
	urlStr := fmt.Sprintf("%sanime/get?id=%d", configAnizium.BaseUrl, *params.Id)
	data, err := internal.GetJson(urlStr, getHeaders())
	if err != nil {
		return nil, err
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("API yanıtı geçersiz")
	}
	animeData, ok := dataMap["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("anime data eksik")
	}

	// tmdb_id hem sayı (95479) hem metin slug ("one-pace") olabilir
	tmdbID := 0
	tmdbSlug := ""
	if tmdbRaw, ok := animeData["tmdb_id"]; ok && tmdbRaw != nil {
		switch v := tmdbRaw.(type) {
		case float64:
			tmdbID = int(v)
			tmdbSlug = fmt.Sprintf("%d", tmdbID)
		case string:
			// Önce sayıya çevirmeyi dene
			fmt.Sscanf(v, "%d", &tmdbID)
			if tmdbID == 0 {
				// Slug tabanlı (örn: "one-pace")
				tmdbSlug = v
			} else {
				tmdbSlug = fmt.Sprintf("%d", tmdbID)
			}
		}
	}

	if tmdbSlug == "" {
		return nil, fmt.Errorf("bu animenin tmdb_id bilgisi eksik, stream bağlantısı çözülemedi")
	}

	// quality artık API yeni yönteminde kullanılmıyor (FetchVideoGroups direkt kalite listesi dönüyor)
	_ = animeData["quality"]

	seasonIdx := 0
	episodeIdx := 0
	skipSoundPref := false

	if params.Extra != nil {
		pExtra := *params.Extra
		if sn, ok := pExtra["seasonIndex"].(int); ok {
			seasonIdx = sn
		}
		if en, ok := pExtra["episodeIndex"].(int); ok {
			episodeIdx = en
		}
		if skip, ok := pExtra["skip_sound_preference"].(bool); ok {
			skipSoundPref = skip
		}
	}

	seasonNum := seasonIdx + 1
	episodeNum := episodeIdx + 1

	// Gerçek sezon/bölüm numaralarını flat indeksten bul
	if seasonsRaw, ok := animeData["seasons"].([]interface{}); ok {
		flatCounter := 0
		found := false
		for _, sRaw := range seasonsRaw {
			sMap, ok := sRaw.(map[string]interface{})
			if !ok {
				continue
			}
			sNum := 1
			if sn, ok := sMap["number"].(float64); ok {
				sNum = int(sn)
			}
			if epsRaw, ok := sMap["episodes"].([]interface{}); ok {
				for _, eRaw := range epsRaw {
					if flatCounter == episodeIdx {
						seasonNum = sNum
						eMap, ok := eRaw.(map[string]interface{})
						if ok {
							if en, ok := eMap["number"].(float64); ok {
								episodeNum = int(en)
							}
						}
						found = true
						break
					}
					flatCounter++
				}
			}
			if found {
				break
			}
		}
	}

	// Ses grupları API'den al
	var soundGroups []string
	if sgRaw, ok := animeData["sound_group"].([]interface{}); ok {
		for _, sg := range sgRaw {
			if sgMap, ok := sg.(map[string]interface{}); ok {
				if val, ok := sgMap["value"].(string); ok {
					soundGroups = append(soundGroups, val)
				}
			}
		}
	}
	if len(soundGroups) == 0 {
		soundGroups = []string{"original", "trdub", "endub"}
	}

	// Değişken tanımlamaları
	var labels []string
	var urls []string
	var trCaption *string
	var watchSubtitles []models.WatchSubtitle
	var warnMessage string

	animeAPIID := 0
	if idRaw, ok := animeData["ID"]; ok {
		switch v := idRaw.(type) {
		case float64:
			animeAPIID = int(v)
		case string:
			fmt.Sscanf(v, "%d", &animeAPIID)
		}
	}

	// Kullanıcı tercihini config'den oku
	preferredStart := "" // boş = en yüksek mevcut
	if appCfg, cfgErr := config.LoadConfig(getConfigPath()); cfgErr == nil && appCfg.PreferredQuality != "" {
		switch appCfg.PreferredQuality {
		case "4K":
			preferredStart = "4K"
		case "2K":
			preferredStart = "2K"
		case "1080p":
			preferredStart = "1080p"
		case "720p":
			preferredStart = "720p"
		case "480p":
			preferredStart = "480p"
		}
	}

	// Kalite öncelik sırası (yüksekten düşüğe)
	qualityOrder := map[string]int{"4K": 5, "2K": 4, "1080p": 3, "720p": 2, "480p": 1}

	// API'den tüm video kaynaklarını tek çağrıyla al
	cfg, cfgErr := LoadConfig()
	if cfgErr != nil || cfg == nil || cfg.Token == "" {
		return nil, fmt.Errorf("Anizium oturumu bulunamadı, lütfen giriş yapın")
	}

	apiVideos, apiSubs, nextEpData, openingData, endingData, apiErr := FetchVideoGroups(cfg, animeAPIID, seasonNum, episodeNum)
	if apiErr != nil {
		return nil, fmt.Errorf("video kaynağı alınamadı: %w", apiErr)
	}

	// Altyazıları watchSubtitles'a aktar
	for _, s := range apiSubs {
		watchSubtitles = append(watchSubtitles, models.WatchSubtitle{
			Group: s.Group,
			Label: s.Label,
			Link:  s.Link,
		})
	}
	// Türkçe altyazıyı TRCaption olarak indir (varsa)
	for _, s := range apiSubs {
		if s.Group == "tr" {
			if tmpPath, dlErr := DownloadVTT(s.Link); dlErr == nil {
				trCaption = &tmpPath
			}
			break
		}
	}

	// Ses tercihini oku (Sor/download modunda atla)
	preferredSound := ""
	if !skipSoundPref {
		if appCfg, cfgErr := config.LoadConfig(getConfigPath()); cfgErr == nil {
			preferredSound = appCfg.PreferredSound
		}
	}

	// API'den gelen videoları kalite tercihine göre filtrele
	// allCDNQualities sıralamasıyla: yüksekten düşüğe
	orderedQualities := []string{"4K", "2K", "1080p", "720p", "480p"}

	// Tercih edilen kaliteden başlayarak uygun kaliteleri filtrele
	startQualityRank := 0
	if preferredStart != "" {
		if rank, ok := qualityOrder[preferredStart]; ok {
			startQualityRank = rank
		}
	}

	// Her kalite grubu için URL'leri topla (yüksekten düşüğe)
	type qualGroup struct {
		qualLabel string
		items     []AnimeVideoSource
	}
	qualMap := map[string]*qualGroup{}
	for _, q := range orderedQualities {
		qualMap[q] = &qualGroup{qualLabel: q}
	}
	for _, v := range apiVideos {
		if qg, ok := qualMap[v.Quality]; ok {
			qg.items = append(qg.items, v)
		}
	}

	// Kalite sıralamasıyla işle
	for _, ql := range orderedQualities {
		qg := qualMap[ql]
		if len(qg.items) == 0 {
			continue
		}
		// Kalite tercihi filtresi
		if preferredStart != "" {
			if qualityOrder[ql] > qualityOrder[preferredStart] {
				continue // tercihten yüksek kaliteyi atla
			}
		}
		// startQualityRank = 0 ise tüm kaliteler dahil (Sor modu veya tercih yok)
		if startQualityRank > 0 && qualityOrder[ql] < startQualityRank {
			if !skipSoundPref {
				continue // tercihten düşük kaliteyi atla (oynatma modunda)
			}
		}

		// Bu kalitedeki sesleri topla
		var qualLabels []string
		var qualURLs []string
		var qualSounds []string
		for _, item := range qg.items {
			qualLabels = append(qualLabels, item.Label)
			qualURLs = append(qualURLs, item.URL)
			qualSounds = append(qualSounds, item.Sound)
		}

		// Ses tercihini uygula
		if preferredSound != "" {
			fallbackSounds := []string{preferredSound, "endub", "cndub", "trdub", "original"}
			seen := map[string]bool{}
			var orderedSounds []string
			for _, s := range fallbackSounds {
				if !seen[s] {
					seen[s] = true
					orderedSounds = append(orderedSounds, s)
				}
			}
			selectedSound := ""
			for _, trySound := range orderedSounds {
				for i, snd := range qualSounds {
					if snd == trySound {
						labels = append(labels, qualLabels[i])
						urls = append(urls, qualURLs[i])
						selectedSound = trySound
						break
					}
				}
				if selectedSound != "" {
					break
				}
			}
			if selectedSound == "" {
				labels = append(labels, qualLabels...)
				urls = append(urls, qualURLs...)
				warnMessage = fmt.Sprintf("⚠️  Tercih edilen ses (%s) ve yedekler bulunamadı, tüm seçenekler listeleniyor.", soundHumanLabel(preferredSound))
			} else if selectedSound != preferredSound {
				warnMessage = fmt.Sprintf("⚠️  %s bulunamadı, %s ile açılıyor.", soundHumanLabel(preferredSound), soundHumanLabel(selectedSound))
			}
		} else {
			labels = append(labels, qualLabels...)
			urls = append(urls, qualURLs...)
		}

		// Oynatma modunda: en yüksek kalitede bul, dur
		if !skipSoundPref && len(urls) > 0 {
			break
		}
	}

	if len(urls) == 0 {
		return nil, fmt.Errorf("video bulunamadı (aniziumID=%d sezon=%d bolum=%d)", animeAPIID, seasonNum, episodeNum)
	}

	// Opening verisini modele dönüştür
	var opening *models.OpeningData
	if openingData != nil {
		opening = &models.OpeningData{
			Start: openingData.Start,
			End:   openingData.End,
		}
	}

	// Ending verisini modele dönüştür
	var ending *models.OpeningData
	if endingData != nil {
		ending = &models.OpeningData{
			Start: endingData.Start,
			End:   endingData.End,
		}
	}

	// NextEpisodeData'yı modele dönüştür
	var nextEpisode *models.NextEpisodeData
	if nextEpData != nil {
		nextEpisode = &models.NextEpisodeData{
			Season:  nextEpData.Season,
			Episode: nextEpData.Episode,
		}
	}

	w := models.Watch{
		Urls:        urls,
		Labels:      labels,
		TRCaption:   trCaption,
		Subtitles:   watchSubtitles,
		WarnMessage: warnMessage,
		NextEpisode: nextEpisode,
		Opening:     opening,
		Ending:      ending,
	}

	return []models.Watch{w}, nil
}

// soundHumanLabel ses kodunu okunabilir Türkçe isme çevirir.
func soundHumanLabel(s string) string {
	switch strings.ToLower(s) {
	case "original":
		return "Japonca (Orijinal)"
	case "trdub":
		return "Türkçe Dublaj"
	case "endub":
		return "İngilizce Dublaj"
	case "cndub":
		return "Çince Dublaj"
	case "trsub":
		return "Türkçe Altyazı"
	default:
		return s
	}
}
