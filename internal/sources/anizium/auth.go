package anizium

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/prayjofir/anitr-cli/internal/helpers"
)

// AniziumConfig - kullanıcı oturum bilgileri, diske kaydedilir
type AniziumConfig struct {
	Email  string `json:"email"`
	UserID string `json:"user_id"`
	Token  string `json:"token"`
	Plan   string `json:"plan"`
}

// configFilePath — Anizium oturum bilgilerinin saklandığı dosyanın yolunu döner
func configFilePath() string {
	return filepath.Join(helpers.ConfigDir(), "anizium.json")
}

// LoadConfig - disk'ten config'i yükler
func LoadConfig() (*AniziumConfig, error) {
	data, err := os.ReadFile(configFilePath())
	if err != nil {
		return nil, err
	}
	var cfg AniziumConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveConfig - config'i diske yazar
func SaveConfig(cfg *AniziumConfig) error {
	path := configFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// AniziumProfile temsilcisi
type AniziumProfile struct {
	ID   string
	Name string
	Pin  string
}

// Login - Anizium API'sine kullanıcı adı/email ve şifre ile giriş yapar, profilleri ve tokeni döner
func Login(usernameOrEmail, password string) ([]AniziumProfile, string, error) {
	clientKey := "16ghkdz5qnwinkyebwopbd94b49xhs"
	
	payloadMap := map[string]interface{}{
		"value":    usernameOrEmail,
		"password": password,
		"date":     time.Now().UnixMilli(),
	}
	payloadBytes, _ := json.Marshal(payloadMap)
	
	encryptedBody := xorEncrypt(string(payloadBytes), clientKey)
	
	finalPayload, _ := json.Marshal(map[string]string{
		"d": encryptedBody,
	})

	// Adım 1: Login ol ve session'ı al
	req, err := http.NewRequest("POST", "https://api.anizium.co/user/login", bytes.NewBuffer(finalPayload))
	if err != nil {
		return nil, "", err
	}

	cfToken := generateCfToken()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Cf-Control", cfToken)
	req.Header.Set("device", "browser")
	req.Header.Set("language", "tr")
	req.Header.Set("site", "main")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("bağlantı hatası: %w", err)
	}
	
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, "", fmt.Errorf("yanıt parse edilemedi: %s", string(body)[:100])
	}

	if isErr, ok := result["isError"].(bool); ok && isErr {
		msg, _ := result["msg"].(string)
		return nil, "", fmt.Errorf("%s", msg)
	}

	if success, ok := result["success"].(bool); ok && !success {
		msg, _ := result["msg"].(string)
		return nil, "", fmt.Errorf("%s", msg)
	}

	sessionToken, _ := result["session"].(string)
	if sessionToken == "" {
		return nil, "", fmt.Errorf("session token alınamadı. API yanıtı: %s", string(body)[:200])
	}

	// Adım 2: sessionToken ile /user/get isteği atarak profilleri çek
	reqGet, err := http.NewRequest("GET", "https://api.anizium.co/user/get", nil)
	if err != nil {
		return nil, "", err
	}
	
	reqGet.Header.Set("Accept", "application/json")
	reqGet.Header.Set("User-Agent", "Mozilla/5.0")
	reqGet.Header.Set("Cf-Control", generateCfToken())
	reqGet.Header.Set("device", "browser")
	reqGet.Header.Set("language", "tr")
	reqGet.Header.Set("site", "main")
	reqGet.Header.Set("Authorization", "Bearer " + sessionToken)
	reqGet.Header.Set("user-session", sessionToken)

	respGet, err := client.Do(reqGet)
	if err != nil {
		return nil, "", fmt.Errorf("profil getirme bağlantı hatası: %w", err)
	}
	defer respGet.Body.Close()
	
	bodyGet, _ := io.ReadAll(respGet.Body)
	
	var resultGet map[string]interface{}
	if err := json.Unmarshal(bodyGet, &resultGet); err != nil {
		return nil, "", fmt.Errorf("profil yanıtı parse edilemedi: %s", string(bodyGet)[:100])
	}

	var availableProfiles []AniziumProfile
	
	if dataObj, ok := resultGet["data"].(map[string]interface{}); ok {
		if profiles, ok := dataObj["profiles"].([]interface{}); ok {
			for _, pRaw := range profiles {
				pMap, ok := pRaw.(map[string]interface{})
				if !ok {
					continue
				}
				prof := AniziumProfile{}
				if pid, ok := pMap["ID"].(string); ok {
					prof.ID = pid
				} else if pidf, ok := pMap["ID"].(float64); ok {
					prof.ID = fmt.Sprintf("%.0f", pidf)
				}
				if pname, ok := pMap["name"].(string); ok {
					prof.Name = pname
				} else {
					prof.Name = "Bilinmeyen Profil"
				}
				if pinVal, ok := pMap["pin"].(string); ok && pinVal != "" {
					prof.Pin = pinVal
				}
				if prof.ID != "" {
					availableProfiles = append(availableProfiles, prof)
				}
			}
		}
		
		// Eğer profil dizisi boşsa ana User ID'yi tek profil gibi ekle
		if len(availableProfiles) == 0 {
			userID := ""
			if id, ok := dataObj["ID"].(string); ok {
				userID = id
			} else if idf, ok := dataObj["ID"].(float64); ok {
				userID = fmt.Sprintf("%.0f", idf)
			}
			if userID != "" {
				availableProfiles = append(availableProfiles, AniziumProfile{
					ID:   userID,
					Name: "Ana Profil",
				})
			}
		}
	}

	if len(availableProfiles) == 0 {
		return nil, "", fmt.Errorf("profil okunamadı. API yanıtı: %s", string(bodyGet)[:300])
	}

	return availableProfiles, sessionToken, nil
}

// GetPlanFromUserGet - session token kullanarak kullanicinin premium plan ID'sini alir
func GetPlanFromUserGet(sessionToken string) string {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", "https://api.anizium.co/user/get", nil)
	if err != nil {
		return "standart"
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Cf-Control", generateCfToken())
	req.Header.Set("device", "browser")
	req.Header.Set("language", "tr")
	req.Header.Set("site", "main")
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	req.Header.Set("user-session", sessionToken)

	resp, err := client.Do(req)
	if err != nil {
		return "standart"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil {
		return "standart"
	}
	if d, ok := result["data"].(map[string]interface{}); ok {
		if pp, ok := d["premium_plan"].(map[string]interface{}); ok {
			if id, ok := pp["ID"].(string); ok && id != "" {
				return id
			}
		}
	}
	return "standart"
}

// GetSubtitleLink - /anime/source endpoint'inden altyazı VTT link'ini al
func GetSubtitleLink(cfg *AniziumConfig, animeID int, seasonNum, episodeNum int, preferredLangs []string) (string, error) {
	plan := cfg.Plan
	if plan == "" {
		plan = "standart"
	}

	url := fmt.Sprintf(
		"https://api.anizium.co/anime/source?id=%d&site=main&plan=%s&season=%d&episode=%d&server=1",
		animeID, plan, seasonNum, episodeNum,
	)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Cf-Control", generateCfToken())
	req.Header.Set("device", "browser")
	req.Header.Set("language", "tr")
	req.Header.Set("site", "main")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("user-session", cfg.Token)
	req.Header.Set("user", cfg.UserID)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	subtitlesRaw, ok := result["subtitles"].([]interface{})
	if !ok || len(subtitlesRaw) == 0 {
		return "", fmt.Errorf("altyazı bulunamadı")
	}

	// Oncelikli dillere gore siralayarak uygun linki bul
	for _, lang := range preferredLangs {
		for _, subRaw := range subtitlesRaw {
			sub, ok := subRaw.(map[string]interface{})
			if !ok {
				continue
			}
			group, _ := sub["group"].(string)
			link, _ := sub["link"].(string)
			if group == lang && link != "" {
				// link'e &type=vtt ekle
				if !strings.Contains(link, "type=") {
					link += "&type=vtt"
				}
				return link, nil
			}
		}
	}

	// Hic eslesen dil yoksa ilk subtitlyi don
	if sub, ok := subtitlesRaw[0].(map[string]interface{}); ok {
		if link, ok := sub["link"].(string); ok && link != "" {
			if !strings.Contains(link, "type=") {
				link += "&type=vtt"
			}
			return link, nil
		}
	}

	return "", fmt.Errorf("altyazı linki bulunamadı")
}

// GetSubtitleNames - oturum token'ı ile embed URL'sine giderek altyazı name'lerini çeker
// Dönen map: dil -> name parametresi (örn: "tr" -> "s3_b12_63249015_535777")
func GetSubtitleNames(cfg *AniziumConfig, episodeID string, seasonNum, episodeNum int) (map[string]string, error) {
	// Embed URL: https://x.anizium.co/embed?u=USER_ID&site=main&lang=tr&id=EPISODE_ID
	embedURL := fmt.Sprintf("https://x.anizium.co/embed?u=%s&site=main&lang=tr&id=%s", cfg.UserID, episodeID)

	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequest("GET", embedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:138.0) Gecko/20100101 Firefox/138.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Referer", "https://anizium.com/")
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Embed sayfasından subtitle API çağrısı yapılıyor
	// Yanıt JS ile doldurulduğundan, embed HTML'ini değil
	// Doğrudan player data API'sini deneyelim
	if len(html) < 5000 && strings.Contains(html, "isError") {
		// Embed doğrudan çalışmadı, API üzerinden dene
		return getSubtitleNamesViaAPI(cfg, episodeID, seasonNum, episodeNum)
	}

	// HTML içinde subtitle name pattern'lerini ara
	// Pattern: s{N}_b{N}_XXXXX_YYYYY
	pattern := regexp.MustCompile(fmt.Sprintf(`s%d_b%d_(\d+)_(\d+)`, seasonNum, episodeNum))
	results := make(map[string]string)

	// Dil bilgisini de almaya çalış - lang=XX ile birlikte gelen name
	langPattern := regexp.MustCompile(`lang[="\s:]+([a-z]{2})[^a-z].*?` + fmt.Sprintf(`s%d_b%d_\d+_\d+`, seasonNum, episodeNum))
	langMatches := langPattern.FindAllStringSubmatch(html, -1)
	for _, m := range langMatches {
		if len(m) >= 2 {
			_ = m[1] // lang kodu
		}
	}

	matches := pattern.FindAllString(html, -1)
	for _, m := range matches {
		results["found"] = m
	}

	if len(results) == 0 {
		return getSubtitleNamesViaAPI(cfg, episodeID, seasonNum, episodeNum)
	}

	return results, nil
}

// getSubtitleNamesViaAPI - token kullanarak API üzerinden altyazı bilgilerini çeker
func getSubtitleNamesViaAPI(cfg *AniziumConfig, episodeID string, seasonNum, episodeNum int) (map[string]string, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	// Authenticated API çağrıları dene
	endpoints := []string{
		fmt.Sprintf("https://api.anizium.co/anime/subtitle?episode_id=%s", episodeID),
		fmt.Sprintf("https://x.anizium.co/api/subtitle/list?id=%s&season=%d&episode=%d", episodeID, seasonNum, episodeNum),
	}

	for _, endpointURL := range endpoints {
		req, err := http.NewRequest("GET", endpointURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Referer", "https://anizium.com/")
		if cfg.Token != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
		}

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			continue
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var result interface{}
		if json.Unmarshal(body, &result) == nil {
			// İçeriği parse etmeye çalış
			resultStr := string(body)
			if strings.Contains(resultStr, fmt.Sprintf("s%d_b%d", seasonNum, episodeNum)) {
				pattern := regexp.MustCompile(fmt.Sprintf(`s%d_b%d_\d+_\d+`, seasonNum, episodeNum))
				if m := pattern.FindString(resultStr); m != "" {
					return map[string]string{"tr": m}, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("altyazı bilgisi bulunamadı")
}

// FetchSubtitleVTT - altyazı name'i ile VTT içeriğini string olarak döner
func FetchSubtitleVTT(animeID int, name string, seasonNum, episodeNum int) (string, error) {
	url := fmt.Sprintf(
		"https://x.anizium.co/api/subtitle/get/file.vtt?id=%d&name=%s&season=%d&episode=%d&type=vtt",
		animeID, name, seasonNum, episodeNum,
	)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://anizium.com/")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	content := string(body)

	if strings.Contains(content, "WEBVTT") {
		return content, nil
	}
	return "", fmt.Errorf("geçerli VTT bulunamadı")
}

// SaveVTTToTemp - VTT içeriğini geçici dosyaya yazar, MPV'ye verilebilir
func SaveVTTToTemp(content string) (string, error) {
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("anitr_sub_%d.vtt", time.Now().UnixMilli()))
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		return "", err
	}
	return tmpFile, nil
}

// --- Yeni çoklu altyazı / API video desteği ---

// AnimeSubtitle, API'den dönen bir altyazı seçeneğini temsil eder.
type AnimeSubtitle struct {
	Group string
	Link  string
	Label string
}

// AnimeVideoSource, /anime/source API'sinden gelen bir video kaynağını temsil eder.
type AnimeVideoSource struct {
	Label   string
	URL     string
	Quality string
	Sound   string
}

// subtitleGroupLabel, dil kodunu okunabilir isme çevirir.
func subtitleGroupLabel(group string) string {
	switch strings.ToLower(group) {
	case "tr":
		return "Türkçe"
	case "en":
		return "İngilizce"
	case "ja":
		return "Japonca"
	case "de":
		return "Almanca"
	case "fr":
		return "Fransızca"
	case "es":
		return "İspanyolca"
	case "ar":
		return "Arapça"
	case "pt":
		return "Portekizce"
	case "it":
		return "İtalyanca"
	default:
		return strings.ToUpper(group)
	}
}

// buildVideoLabel, quality ve sound değerlerinden görünen etiket üretir.
func buildVideoLabel(quality, sound string) string {
	soundLabel := ""
	switch sound {
	case "original":
		soundLabel = "Japonca"
	case "trdub":
		soundLabel = "Türkçe Dublaj"
	case "endub":
		soundLabel = "İngilizce Dublaj"
	case "trsub":
		soundLabel = "Türkçe Altyazı"
	default:
		if sound != "" {
			soundLabel = sound
		}
	}
	if quality != "" && soundLabel != "" {
		return fmt.Sprintf("%s (%s)", quality, soundLabel)
	}
	if quality != "" {
		return quality
	}
	return "Kaynak"
}


// AnimeNextEpisode, API'den dönen bir sonraki bölüm bilgisini temsil eder.
type AnimeNextEpisode struct {
	Season  int
	Episode int
}

// AnimeOpeningTime, API'den dönen intro/opening zaman bilgisini temsil eder.
type AnimeOpeningTime struct {
	Start string // "00:01:01"
	End   string // "00:02:31"
}

// FetchVideoGroups - /anime/source API'sinden "groups" alanını parse ederek
// tüm kalite ve ses kombinasyonlarını döner. Hem video hem altyazı hem de
// sonraki bölüm bilgisi (next_episode_data) döner.
// Bu fonksiyon GetWatchData içinde CDN probe'un yerini almaktadır.
func FetchVideoGroups(cfg *AniziumConfig, animeID, seasonNum, episodeNum int) ([]AnimeVideoSource, []AnimeSubtitle, *AnimeNextEpisode, *AnimeOpeningTime, *AnimeOpeningTime, error) {
	plan := cfg.Plan
	if plan == "" {
		plan = "standart"
	}
	apiURL := fmt.Sprintf(
		"https://api.anizium.co/anime/source?id=%d&site=main&plan=%s&season=%d&episode=%d&server=1",
		animeID, plan, seasonNum, episodeNum,
	)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Cf-Control", generateCfToken())
	req.Header.Set("device", "browser")
	req.Header.Set("language", "tr")
	req.Header.Set("site", "main")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("user-session", cfg.Token)
	req.Header.Set("user", cfg.UserID)

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("API yanıtı parse edilemedi")
	}

	if isErr, _ := result["isError"].(bool); isErr {
		msg, _ := result["msg"].(string)
		return nil, nil, nil, nil, nil, fmt.Errorf("API hatası: %s", msg)
	}

	// Video kaynaklarını "groups" alanından parse et
	// Her group: {group: "trdub", name: "Türkçe", items: [{link, quality, type}]}
	var videos []AnimeVideoSource
	if groupsRaw, ok := result["groups"].([]interface{}); ok {
		for _, gRaw := range groupsRaw {
			g, ok := gRaw.(map[string]interface{})
			if !ok {
				continue
			}
			soundCode, _ := g["group"].(string) // "original", "trdub", "endub"
			if itemsRaw, ok := g["items"].([]interface{}); ok {
				for _, iRaw := range itemsRaw {
					item, ok := iRaw.(map[string]interface{})
					if !ok {
						continue
					}
					link, _ := item["link"].(string)
					if link == "" {
						continue
					}
					qualityF, _ := item["quality"].(float64)
					quality := int(qualityF)

					// Kalite etiketini oluştur
					qualLabel := fmt.Sprintf("%dp", quality)
					switch quality {
					case 2160:
						qualLabel = "4K"
					case 1440:
						qualLabel = "2K"
					}

					videos = append(videos, AnimeVideoSource{
						Label:   buildVideoLabel(qualLabel, soundCode),
						URL:     link,
						Quality: qualLabel,
						Sound:   soundCode,
					})
				}
			}
		}
	}

	// Altyazıları parse et
	var subtitles []AnimeSubtitle
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
			subtitles = append(subtitles, AnimeSubtitle{
				Group: group,
				Link:  link,
				Label: subtitleGroupLabel(group),
			})
		}
	}

	// content alanından next_episode_data, opening ve ending parse et
	var nextEp *AnimeNextEpisode
	var openingTime *AnimeOpeningTime
	var endingTime *AnimeOpeningTime
	if contentRaw, ok := result["content"].(map[string]interface{}); ok {
		// next_episode_data
		if nedRaw, ok := contentRaw["next_episode_data"].(map[string]interface{}); ok {
			ned := &AnimeNextEpisode{}
			if s, ok := nedRaw["season"].(float64); ok {
				ned.Season = int(s)
			}
			if e, ok := nedRaw["episode"].(float64); ok {
				ned.Episode = int(e)
			}
			if ned.Season > 0 && ned.Episode > 0 {
				nextEp = ned
			}
		}
		// opening (intro)
		if opRaw, ok := contentRaw["opening"].(map[string]interface{}); ok {
			start, _ := opRaw["start"].(string)
			end, _ := opRaw["end"].(string)
			if start != "" && end != "" {
				openingTime = &AnimeOpeningTime{Start: start, End: end}
			}
		}
		// ending (outro)
		if edRaw, ok := contentRaw["ending"].(map[string]interface{}); ok {
			start, _ := edRaw["start"].(string)
			end, _ := edRaw["end"].(string)
			if start != "" && end != "" {
				endingTime = &AnimeOpeningTime{Start: start, End: end}
			}
		}
	}

	if len(videos) == 0 {
		return nil, subtitles, nextEp, openingTime, endingTime, fmt.Errorf("API'den video kaynağı alınamadı")
	}

	return videos, subtitles, nextEp, openingTime, endingTime, nil
}

// DownloadVTT - bir VTT linkini indirir ve /tmp'ye kaydeder, yolu doner.
func DownloadVTT(link string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(link)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return SaveVTTToTemp(string(data))
}
