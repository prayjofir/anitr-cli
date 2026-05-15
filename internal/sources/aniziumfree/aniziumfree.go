package aniziumfree

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/prayjofir/anitr-cli/internal"
	"github.com/prayjofir/anitr-cli/internal/config"
	"github.com/prayjofir/anitr-cli/internal/helpers"
	"github.com/prayjofir/anitr-cli/internal/models"
	"github.com/prayjofir/anitr-cli/internal/sources/anizium"
)

// AniziumFree — hesap gerektirmeyen Anizium kaynağı.
// Video: CDN probe (paralel HEAD isteği), Altyazı: /anime/source token'sız deneme.
type AniziumFree struct{}

func (a AniziumFree) Source() string { return "Anizium Free" }

// ── Şifreleme / CF token (anizium package'dan kopyalandı, unexported olduğu için) ──

func xorEncrypt(text, key string) string {
	tb := []byte(text)
	kb := []byte(key)
	res := make([]byte, len(tb))
	for i, b := range tb {
		res[i] = b ^ kb[i%len(kb)]
	}
	return fmt.Sprintf("%x", res)
}

func randomString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}

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

func baseHeaders() map[string]string {
	return map[string]string{
		"Accept":     "application/json",
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
		"Cf-Control": generateCfToken(),
		"device":     "browser",
		"language":   "tr",
		"site":       "main",
	}
}

func getConfigPath() string {
	return filepath.Join(helpers.ConfigDir(), "config.json")
}

var configFree = internal.Config{
	BaseUrl: "https://api.anizium.co/",
}

// ── Arama ──────────────────────────────────────────────────────────────────────

func (a AniziumFree) GetSearchData(query string) ([]models.Anime, error) {
	searchURL := fmt.Sprintf("%spage/search?value=%s&page=1", configFree.BaseUrl, url.QueryEscape(query))
	data, err := internal.GetJson(searchURL, baseHeaders())
	if err != nil {
		return nil, fmt.Errorf("arama verisi alınamadı: %w", err)
	}
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("yanıt formatı geçersiz")
	}
	pageMap, ok := dataMap["page"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("'page' alanı bulunamadı")
	}
	itemsRaw, ok := pageMap["data"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("'data' alanı bulunamadı")
	}

	var result []models.Anime
	for _, item := range itemsRaw {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		title, _ := itemMap["name"].(string)
		slug, _ := itemMap["sef"].(string)
		poster, _ := itemMap["poster"].(string)
		animeType, _ := itemMap["type"].(string)

		var id int
		if idStr, ok := itemMap["ID"].(string); ok {
			fmt.Sscanf(idStr, "%d", &id)
		} else if idF, ok := itemMap["ID"].(float64); ok {
			id = int(idF)
		}
		var tmdbID int
		if tmdbStr, ok := itemMap["tmdb_id"].(string); ok {
			fmt.Sscanf(tmdbStr, "%d", &tmdbID)
		} else if tmdbF, ok := itemMap["tmdb_id"].(float64); ok {
			tmdbID = int(tmdbF)
		}

		extra := map[string]interface{}{"tmdb_id": tmdbID}
		result = append(result, models.Anime{
			Title:     title,
			Slug:      &slug,
			ID:        &id,
			ImageURL:  poster,
			Type:      &animeType,
			TitleType: &animeType,
			Extra:     extra,
		})
	}
	return result, nil
}

// ── Sezon ──────────────────────────────────────────────────────────────────────

func (a AniziumFree) GetAnimeByID(id string) (*models.Anime, error) {
	urlStr := fmt.Sprintf("%sanime/get?id=%s", configFree.BaseUrl, id)
	data, err := internal.GetJson(urlStr, baseHeaders())
	if err != nil {
		return nil, err
	}
	dataMap, _ := data.(map[string]interface{})
	itemMap, _ := dataMap["data"].(map[string]interface{})
	title, _ := itemMap["name"].(string)
	return &models.Anime{Title: title}, nil
}

func (a AniziumFree) GetSeasonsData(params models.SeasonParams) ([]models.Season, error) {
	if params.Id == nil {
		return nil, fmt.Errorf("anime id gerekli")
	}
	urlStr := fmt.Sprintf("%sanime/get?id=%d", configFree.BaseUrl, *params.Id)
	data, err := internal.GetJson(urlStr, baseHeaders())
	if err != nil {
		return nil, err
	}
	dataMap, _ := data.(map[string]interface{})
	animeData, _ := dataMap["data"].(map[string]interface{})
	seasonsRaw, _ := animeData["seasons"].([]interface{})

	var numbers []int
	for _, sRaw := range seasonsRaw {
		sMap, ok := sRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if n, ok := sMap["number"].(float64); ok {
			numbers = append(numbers, int(n))
		}
	}
	count := len(numbers)
	isMovie := animeData["type"] == "movie"
	return []models.Season{{Seasons: &numbers, Count: &count, IsMovie: &isMovie}}, nil
}

// ── Bölümler ───────────────────────────────────────────────────────────────────

func (a AniziumFree) GetEpisodesData(params models.EpisodeParams) ([]models.Episode, error) {
	if params.SeasonID == nil {
		return nil, fmt.Errorf("anime id gerekli")
	}
	urlStr := fmt.Sprintf("%sanime/get?id=%d", configFree.BaseUrl, *params.SeasonID)
	data, err := internal.GetJson(urlStr, baseHeaders())
	if err != nil {
		return nil, err
	}
	dataMap, _ := data.(map[string]interface{})
	animeData, _ := dataMap["data"].(map[string]interface{})

	var tmdbID int
	if tmdbStr, ok := animeData["tmdb_id"].(string); ok {
		fmt.Sscanf(tmdbStr, "%d", &tmdbID)
	} else if tmdbF, ok := animeData["tmdb_id"].(float64); ok {
		tmdbID = int(tmdbF)
	}

	seasonsRaw, _ := animeData["seasons"].([]interface{})
	var epList []models.Episode
	for _, sRaw := range seasonsRaw {
		sMap, ok := sRaw.(map[string]interface{})
		if !ok {
			continue
		}
		seasonNum := 1
		if sn, ok := sMap["number"].(float64); ok {
			seasonNum = int(sn)
		}
		episodesRaw, _ := sMap["episodes"].([]interface{})
		for _, eRaw := range episodesRaw {
			eMap, ok := eRaw.(map[string]interface{})
			if !ok {
				continue
			}
			title, _ := eMap["name"].(string)
			if title == "" {
				title = "Bölüm"
			}
			var epNum int
			if n, ok := eMap["number"].(float64); ok {
				epNum = int(n)
			}
			extra := map[string]interface{}{
				"season_num":  float64(seasonNum),
				"episode_num": epNum,
				"tmdb_id":     tmdbID,
			}
			epList = append(epList, models.Episode{
				ID:     fmt.Sprintf("%v", eMap["ID"]),
				Title:  fmt.Sprintf("%d - %s", epNum, title),
				Number: epNum,
				Extra:  extra,
			})
		}
	}
	return epList, nil
}

// ── CDN Probe (Paralel) ────────────────────────────────────────────────────────

var cdnServers = []string{
	"https://a.aniziumserver.sbs", "https://a.aniziumserver.site",
	"https://f.aniziumserver.sbs", "https://f.aniziumserver.site",
	"https://k.aniziumserver.sbs", "https://k.aniziumserver.site",
	"https://r.aniziumserver.sbs", "https://r.aniziumserver.site",
	"https://u.aniziumserver.sbs", "https://u.aniziumserver.site",
	"https://x.aniziumserver.sbs", "https://x.aniziumserver.site",
}

var cdnQualities = []string{"2160p", "1440p", "1080p", "720p", "480p"}
var cdnSounds = []string{"original", "trdub", "endub", "trsub"}

func buildLabel(quality, sound string) string {
	soundLabel := map[string]string{
		"original": "Japonca",
		"trdub":    "Türkçe Dublaj",
		"endub":    "İngilizce Dublaj",
		"trsub":    "Türkçe Altyazı",
	}[sound]
	if soundLabel == "" {
		soundLabel = sound
	}
	if quality != "" && soundLabel != "" {
		return fmt.Sprintf("%s (%s)", quality, soundLabel)
	}
	return quality
}

func qualLabel(q string) string {
	switch q {
	case "2160p":
		return "4K"
	case "1440p":
		return "2K"
	default:
		return q
	}
}

type probeResult struct {
	qualIdx  int
	soundIdx int
	src      anizium.AnimeVideoSource
}

// probeCDNParallel — tüm kalite+ses kombinasyonlarını sunuculara paralel HEAD ile tarar.
func probeCDNParallel(tmdbSlug string, seasonNum, episodeNum int, isMovie bool) []anizium.AnimeVideoSource {
	resultCh := make(chan probeResult, len(cdnQualities)*len(cdnSounds))
	var wg sync.WaitGroup

	for qi, qual := range cdnQualities {
		for si, sound := range cdnSounds {
			wg.Add(1)
			go func(qi, si int, qual, sound string) {
				defer wg.Done()
				client := &http.Client{Timeout: 5 * time.Second}
				for _, srv := range cdnServers {
					var streamURL string
					if isMovie {
						streamURL = fmt.Sprintf("%s/%s/%s-%s/master.m3u8", srv, tmdbSlug, qual, sound)
					} else {
						streamURL = fmt.Sprintf("%s/%s/%d/%d/%s-%s/master.m3u8",
							srv, tmdbSlug, seasonNum, episodeNum, qual, sound)
					}
					req, err := http.NewRequest("HEAD", streamURL, nil)
					if err != nil {
						continue
					}
					req.Header.Set("User-Agent", "Mozilla/5.0")
					resp, err := client.Do(req)
					if err == nil && resp.StatusCode == 200 {
						ql := qualLabel(qual)
						resultCh <- probeResult{
							qualIdx:  qi,
							soundIdx: si,
							src: anizium.AnimeVideoSource{
								Label:   buildLabel(ql, sound),
								URL:     streamURL,
								Quality: ql,
								Sound:   sound,
							},
						}
						return
					}
				}
			}(qi, si, qual, sound)
		}
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Sonuçları topla, kalite sırasına göre sırala (yüksekten düşüğe)
	total := len(cdnQualities) * len(cdnSounds)
	ordered := make([]*anizium.AnimeVideoSource, total)
	for r := range resultCh {
		idx := r.qualIdx*len(cdnSounds) + r.soundIdx
		src := r.src
		ordered[idx] = &src
	}
	var results []anizium.AnimeVideoSource
	for _, src := range ordered {
		if src != nil {
			results = append(results, *src)
		}
	}
	return results
}

// ── Altyazı (token'sız deneme) ─────────────────────────────────────────────────

type freeSubResult struct {
	subtitles   []anizium.AnimeSubtitle
	nextEp      *anizium.AnimeNextEpisode
	openingTime *anizium.AnimeOpeningTime
	endingTime  *anizium.AnimeOpeningTime
}

// fetchSourceNoAuth — /anime/source endpoint'ini token olmadan çağırır.
// Altyazı ve içerik verisi (opening/ending/next_ep) döndürmeye çalışır.
func fetchSourceNoAuth(animeAPIID, seasonNum, episodeNum int) freeSubResult {
	apiURL := fmt.Sprintf(
		"https://api.anizium.co/anime/source?id=%d&site=main&plan=standart&season=%d&episode=%d&server=1",
		animeAPIID, seasonNum, episodeNum,
	)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return freeSubResult{}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Cf-Control", generateCfToken())
	req.Header.Set("device", "browser")
	req.Header.Set("language", "tr")
	req.Header.Set("site", "main")
	// Token yok — hesapsız deneme

	resp, err := client.Do(req)
	if err != nil {
		return freeSubResult{}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return freeSubResult{}
	}

	var out freeSubResult

	// Altyazılar
	if subsRaw, ok := result["subtitles"].([]interface{}); ok {
		for _, subRaw := range subsRaw {
			sub, ok := subRaw.(map[string]interface{})
			if !ok {
				continue
			}
			group, _ := sub["group"].(string)
			link, _ := sub["link"].(string)
			if group == "" || link == "" {
				continue
			}
			if !strings.Contains(link, "type=") {
				link += "&type=vtt"
			}
			out.subtitles = append(out.subtitles, anizium.AnimeSubtitle{
				Group: group,
				Link:  link,
				Label: subtitleLabel(group),
			})
		}
	}

	// content → next_episode_data, opening, ending
	if contentRaw, ok := result["content"].(map[string]interface{}); ok {
		if nedRaw, ok := contentRaw["next_episode_data"].(map[string]interface{}); ok {
			ned := &anizium.AnimeNextEpisode{}
			if s, ok := nedRaw["season"].(float64); ok {
				ned.Season = int(s)
			}
			if e, ok := nedRaw["episode"].(float64); ok {
				ned.Episode = int(e)
			}
			if ned.Season > 0 && ned.Episode > 0 {
				out.nextEp = ned
			}
		}
		if opRaw, ok := contentRaw["opening"].(map[string]interface{}); ok {
			start, _ := opRaw["start"].(string)
			end, _ := opRaw["end"].(string)
			if start != "" && end != "" {
				out.openingTime = &anizium.AnimeOpeningTime{Start: start, End: end}
			}
		}
		if edRaw, ok := contentRaw["ending"].(map[string]interface{}); ok {
			start, _ := edRaw["start"].(string)
			end, _ := edRaw["end"].(string)
			if start != "" && end != "" {
				out.endingTime = &anizium.AnimeOpeningTime{Start: start, End: end}
			}
		}
	}

	return out
}

// ── GetWatchData ───────────────────────────────────────────────────────────────

func (a AniziumFree) GetWatchData(params models.WatchParams) ([]models.Watch, error) {
	if params.Id == nil {
		return nil, fmt.Errorf("anime id eksik")
	}

	// Anime detayını çek (auth yok)
	urlStr := fmt.Sprintf("%sanime/get?id=%d", configFree.BaseUrl, *params.Id)
	data, err := internal.GetJson(urlStr, baseHeaders())
	if err != nil {
		return nil, err
	}
	dataMap, _ := data.(map[string]interface{})
	animeData, ok := dataMap["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("anime data eksik")
	}

	// TMDB slug'ı bul
	tmdbSlug := ""
	tmdbID := 0
	if tmdbRaw, ok := animeData["tmdb_id"]; ok && tmdbRaw != nil {
		switch v := tmdbRaw.(type) {
		case float64:
			tmdbID = int(v)
			tmdbSlug = fmt.Sprintf("%d", tmdbID)
		case string:
			fmt.Sscanf(v, "%d", &tmdbID)
			if tmdbID == 0 {
				tmdbSlug = v
			} else {
				tmdbSlug = fmt.Sprintf("%d", tmdbID)
			}
		}
	}
	if tmdbSlug == "" {
		return nil, fmt.Errorf("tmdb_id bilgisi eksik, CDN URL'si oluşturulamadı")
	}

	// Anizium internal API ID
	animeAPIID := 0
	if idRaw, ok := animeData["ID"]; ok {
		switch v := idRaw.(type) {
		case float64:
			animeAPIID = int(v)
		case string:
			fmt.Sscanf(v, "%d", &animeAPIID)
		}
	}

	// Extra'dan index'leri oku
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

	// Flat index → gerçek sezon/bölüm numarası
	seasonNum := seasonIdx + 1
	episodeNum := episodeIdx + 1
	isMovie := false
	if animeData["type"] == "movie" {
		isMovie = true
	}

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

	// Kalite/ses tercihini oku
	preferredQuality := ""
	preferredSound := ""
	if appCfg, err := config.LoadConfig(getConfigPath()); err == nil {
		preferredQuality = appCfg.PreferredQuality
		if !skipSoundPref {
			preferredSound = appCfg.PreferredSound
		}
	}

	// Video (CDN probe) ve altyazı (auth'suz) paralel başlat
	type videoRes struct {
		sources []anizium.AnimeVideoSource
	}
	videoCh := make(chan videoRes, 1)
	subCh := make(chan freeSubResult, 1)

	go func() {
		videoCh <- videoRes{probeCDNParallel(tmdbSlug, seasonNum, episodeNum, isMovie)}
	}()
	go func() {
		if animeAPIID > 0 {
			raw := fetchSourceNoAuth(animeAPIID, seasonNum, episodeNum)
			// Eğer API altyazı döndürdüyse → discovery ile tüm sunucu+dil varyasyonlarını dene
			if len(raw.subtitles) > 0 {
				knownSub := raw.subtitles[0]
				discovered := DiscoverAndDownloadSubtitles(knownSub.Link, knownSub.Group)
				// Discovery sonuçlarını raw.subtitles ile birleştir
				seen := map[string]bool{}
				var merged []anizium.AnimeSubtitle
				for _, d := range discovered {
					if !seen[d.Group] {
						seen[d.Group] = true
						merged = append(merged, anizium.AnimeSubtitle{
							Group: d.Group,
							Link:  d.URL,
							Label: d.Label,
						})
					}
				}
				// raw'dan gelen ama discovery'de olmayan altyazıları da ekle
				for _, s := range raw.subtitles {
					if !seen[s.Group] {
						seen[s.Group] = true
						merged = append(merged, s)
					}
				}
				raw.subtitles = merged
			}
			subCh <- raw
		} else {
			subCh <- freeSubResult{}
		}
	}()

	videoResult := <-videoCh
	subResult := <-subCh

	apiVideos := videoResult.sources

	if len(apiVideos) == 0 {
		return nil, fmt.Errorf("CDN'de video bulunamadı (tmdb=%s sezon=%d bolum=%d) — CDN sunucularına erişim olmayabilir", tmdbSlug, seasonNum, episodeNum)
	}

	// Kalite sıralama
	qualityOrder := map[string]int{"4K": 5, "2K": 4, "1080p": 3, "720p": 2, "480p": 1}
	orderedQualities := []string{"4K", "2K", "1080p", "720p", "480p"}

	type qualGroup struct {
		items []anizium.AnimeVideoSource
	}
	qualMap := map[string]*qualGroup{}
	for _, q := range orderedQualities {
		qualMap[q] = &qualGroup{}
	}
	for _, v := range apiVideos {
		if qg, ok := qualMap[v.Quality]; ok {
			qg.items = append(qg.items, v)
		}
	}

	var labels []string
	var urls []string
	var warnMessage string

	for _, ql := range orderedQualities {
		qg := qualMap[ql]
		if len(qg.items) == 0 {
			continue
		}
		if preferredQuality != "" && preferredQuality != "Sor" {
			if qualityOrder[ql] > qualityOrder[preferredQuality] {
				continue
			}
		}

		var qualLabels []string
		var qualURLs []string
		var qualSounds []string
		for _, item := range qg.items {
			qualLabels = append(qualLabels, item.Label)
			qualURLs = append(qualURLs, item.URL)
			qualSounds = append(qualSounds, item.Sound)
		}

		if preferredSound != "" && !skipSoundPref {
			fallback := []string{preferredSound, "endub", "cndub", "trdub", "original"}
			seen := map[string]bool{}
			var ordered []string
			for _, s := range fallback {
				if !seen[s] {
					seen[s] = true
					ordered = append(ordered, s)
				}
			}
			selected := ""
			for _, trySound := range ordered {
				for i, snd := range qualSounds {
					if snd == trySound {
						labels = append(labels, qualLabels[i])
						urls = append(urls, qualURLs[i])
						selected = trySound
						break
					}
				}
				if selected != "" {
					break
				}
			}
			if selected == "" {
				labels = append(labels, qualLabels...)
				urls = append(urls, qualURLs...)
				warnMessage = "⚠️  Tercih edilen ses bulunamadı, tüm seçenekler listeleniyor."
			}
		} else {
			labels = append(labels, qualLabels...)
			urls = append(urls, qualURLs...)
		}

		if !skipSoundPref && len(urls) > 0 {
			break
		}
	}

	if len(urls) == 0 {
		return nil, fmt.Errorf("uygun video bulunamadı")
	}

	// Altyazıları dönüştür
	var watchSubtitles []models.WatchSubtitle
	var trCaption *string

	for _, s := range subResult.subtitles {
		watchSubtitles = append(watchSubtitles, models.WatchSubtitle{
			Group: s.Group,
			Label: s.Label,
			Link:  s.Link,
		})
	}
	for _, s := range subResult.subtitles {
		if s.Group == "tr" {
			if tmpPath, dlErr := anizium.DownloadVTT(s.Link); dlErr == nil {
				trCaption = &tmpPath
			}
			break
		}
	}

	// Altyazı yoksa uyarı ver
	if len(watchSubtitles) == 0 && warnMessage == "" {
		warnMessage = "⚠️  Altyazı alınamadı — bu kaynak hesap gerektiren altyazıları gösteremiyor olabilir."
	}

	// Opening/Ending
	var opening *models.OpeningData
	if subResult.openingTime != nil {
		opening = &models.OpeningData{Start: subResult.openingTime.Start, End: subResult.openingTime.End}
	}
	var ending *models.OpeningData
	if subResult.endingTime != nil {
		ending = &models.OpeningData{Start: subResult.endingTime.Start, End: subResult.endingTime.End}
	}

	// NextEpisode
	var nextEpisode *models.NextEpisodeData
	if subResult.nextEp != nil {
		nextEpisode = &models.NextEpisodeData{
			Season:  subResult.nextEp.Season,
			Episode: subResult.nextEp.Episode,
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
