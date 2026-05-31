package jikan

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnimeDetails represents detailed information about an anime
type AnimeDetails struct {
	Score      float64 `json:"score"`
	Status     string  `json:"status"`
	Synopsis   string  `json:"synopsis"`
	Year       int     `json:"year"`
	Aired      struct {
		From string `json:"from"`
	} `json:"aired"`
	Broadcast  struct {
		String string `json:"string"`
	} `json:"broadcast"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Relations []struct {
		Relation string `json:"relation"`
		Entry    []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"entry"`
	} `json:"relations"`
}

type AnimeDetailsResponse struct {
	Data AnimeDetails `json:"data"`
}

var (
	animeDetailsCache = make(map[int]*AnimeDetails)
)

// GetAnimeDetails fetches full details for a given MAL ID
func GetAnimeDetails(malID int) (*AnimeDetails, error) {
	if details, ok := animeDetailsCache[malID]; ok {
		return details, nil
	}

	apiURL := fmt.Sprintf("https://api.jikan.moe/v4/anime/%d/full", malID)

	client := &http.Client{Timeout: 10 * time.Second}
	var resp *http.Response
	var err error

	for retries := 0; retries < 3; retries++ {
		resp, err = client.Get(apiURL)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			time.Sleep(1000 * time.Millisecond)
			continue
		}
		break
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jikan API error: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var detailResp AnimeDetailsResponse
	if err := json.Unmarshal(body, &detailResp); err != nil {
		return nil, err
	}

	animeDetailsCache[malID] = &detailResp.Data
	return &detailResp.Data, nil
}
