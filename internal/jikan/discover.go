package jikan

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"strconv"
)

type AnimeBasic struct {
	MalID        int     `json:"mal_id"`
	Title        string  `json:"title"`
	TitleEnglish string  `json:"title_english"`
	Titles       []struct {
		Type  string `json:"type"`
		Title string `json:"title"`
	} `json:"titles"`
	Score  float64 `json:"score"`
	Year   int     `json:"year"`
	Aired  struct {
		From string `json:"from"`
	} `json:"aired"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
}

type AnimeListResponse struct {
	Data       []AnimeBasic `json:"data"`
	Pagination struct {
		HasNextPage bool `json:"has_next_page"`
	} `json:"pagination"`
}

type UserWatchlistResponse struct {
	Data []struct {
		Anime AnimeBasic `json:"anime"`
	} `json:"data"`
}

func fetchAnimeListMultiPage(apiURLBase string, maxPages int) ([]AnimeBasic, error) {
	var allAnimes []AnimeBasic
	client := &http.Client{Timeout: 10 * time.Second}

	for page := 1; page <= maxPages; page++ {
		apiURL := fmt.Sprintf("%s&page=%d", apiURLBase, page)
		resp, err := client.Get(apiURL)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			if page == 1 {
				return nil, fmt.Errorf("jikan API error: %s", resp.Status)
			}
			break
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			break
		}

		var listResp AnimeListResponse
		if err := json.Unmarshal(body, &listResp); err != nil {
			break
		}

		allAnimes = append(allAnimes, listResp.Data...)

		if !listResp.Pagination.HasNextPage {
			break
		}

		if page < maxPages {
			time.Sleep(400 * time.Millisecond)
		}
	}

	return allAnimes, nil
}

// GetTopAnime fetches the current top anime
func GetTopAnime() ([]AnimeBasic, error) {
	return fetchAnimeListMultiPage("https://api.jikan.moe/v4/top/anime?limit=25", 4) // 100 anime
}

// GetSeasonalAnime fetches the currently airing seasonal anime
func GetSeasonalAnime() ([]AnimeBasic, error) {
	return fetchAnimeListMultiPage("https://api.jikan.moe/v4/seasons/now?limit=25", 8) // max 200 anime
}

// GetUserWatchlist fetches the "Watching" list for a specific MAL user
type MALItem struct {
	AnimeTitle        string  `json:"anime_title"`
	AnimeID           int     `json:"anime_id"`
	AnimeScoreVal     float64 `json:"anime_score_val"`
	AnimeStartDateStr string  `json:"anime_start_date_string"`
	Genres            []struct {
		Name string `json:"name"`
	} `json:"genres"`
}

// GetUserWatchlist fetches the "Watching" list for a specific MAL user directly from MAL HTML
func GetUserWatchlist(username string) ([]AnimeBasic, error) {
	apiURL := fmt.Sprintf("https://myanimelist.net/animelist/%s?status=1", username)

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", apiURL, nil)
	// Add User-Agent to avoid basic blocks
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("MAL kullanıcısı bulunamadı: %s", username)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MyAnimeList'e ulaşılamadı (HTTP %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile(`data-items="([^"]+)"`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		// It could mean the list is empty or private
		return []AnimeBasic{}, nil
	}

	rawJSON := html.UnescapeString(string(matches[1]))

	var items []MALItem
	if err := json.Unmarshal([]byte(rawJSON), &items); err != nil {
		return nil, err
	}

	var result []AnimeBasic
	for _, entry := range items {
		// Yılı ayrıştır (MM-DD-YY)
		year := 0
		parts := strings.Split(entry.AnimeStartDateStr, "-")
		if len(parts) == 3 {
			yy, err := strconv.Atoi(parts[2])
			if err == nil {
				if yy > 50 {
					year = 1900 + yy
				} else {
					year = 2000 + yy
				}
			}
		}

		// Genre dönüşümü
		var basicGenres []struct {
			Name string `json:"name"`
		}
		for _, g := range entry.Genres {
			basicGenres = append(basicGenres, struct {
				Name string `json:"name"`
			}{Name: g.Name})
		}

		result = append(result, AnimeBasic{
			MalID:  entry.AnimeID,
			Title:  entry.AnimeTitle,
			Score:  entry.AnimeScoreVal,
			Year:   year,
			Genres: basicGenres,
		})
	}

	return result, nil
}
// CleanTitle removes common tags and punctuation that prevent accurate matching.
func CleanTitle(title string) string {
	t := strings.ToLower(title)
	t = strings.ReplaceAll(t, " (tv)", "")
	t = strings.ReplaceAll(t, " (dub)", "")
	t = strings.ReplaceAll(t, " (sub)", "")
	
	// Remove all punctuation
	punctuation := []string{":", "-", "!", "?", ".", ",", ";", "'", "\""}
	for _, p := range punctuation {
		t = strings.ReplaceAll(t, p, " ")
	}

	// Collapse multiple spaces into one
	fields := strings.Fields(t)
	return strings.Join(fields, " ")
}

// MatchAnimeTitle tries to find the best match for a source title in Jikan API results.
func MatchAnimeTitle(sourceTitle string, jikanResults []AnimeBasic) *AnimeBasic {
	cleanSource := CleanTitle(sourceTitle)

	// 1. Exact or cleaned exact match
	for _, j := range jikanResults {
		if CleanTitle(j.Title) == cleanSource || CleanTitle(j.TitleEnglish) == cleanSource {
			return &j
		}
		for _, alt := range j.Titles {
			if CleanTitle(alt.Title) == cleanSource {
				return &j
			}
		}
	}

	// 2. Partial match (contains)
	for _, j := range jikanResults {
		cleanJ := CleanTitle(j.Title)
		cleanEng := CleanTitle(j.TitleEnglish)
		
		if (cleanJ != "" && (strings.Contains(cleanSource, cleanJ) || strings.Contains(cleanJ, cleanSource))) || 
		   (cleanEng != "" && (strings.Contains(cleanSource, cleanEng) || strings.Contains(cleanEng, cleanSource))) {
			return &j
		}
	}

	return nil
}
