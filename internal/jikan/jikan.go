package jikan

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prayjofir/anitr-cli/internal/helpers"
)

type AnimeSearchResponse struct {
	Data []struct {
		MalID int `json:"mal_id"`
	} `json:"data"`
}

type EpisodeFlags struct {
	Filler bool `json:"filler"`
	Recap  bool `json:"recap"`
}

type EpisodesResponse struct {
	Pagination struct {
		HasNextPage bool `json:"has_next_page"`
	} `json:"pagination"`
	Data []struct {
		MalID  int  `json:"mal_id"`
		Filler bool `json:"filler"`
		Recap  bool `json:"recap"`
	} `json:"data"`
}

var (
	malIDCache = make(map[string]int)
)

// SetMalIDCache manually sets the cache for a given title to avoid redundant/mismatched API calls.
func SetMalIDCache(title string, malID int) {
	malIDCache[title] = malID
}

// GetMalIDByTitle searches Jikan API for the anime title and returns the best matched mal_id.
func GetMalIDByTitle(title string) (int, error) {
	if id, ok := malIDCache[title]; ok {
		return id, nil
	}

	results, err := SearchAnime(title)
	if err != nil || len(results) == 0 {
		// Fallback: If title is too long, try searching with just the first 4 words
		words := strings.Fields(title)
		if len(words) > 4 {
			shortTitle := strings.Join(words[:4], " ")
			fallbackResults, fallbackErr := SearchAnime(shortTitle)
			if fallbackErr == nil && len(fallbackResults) > 0 {
				results = fallbackResults
				err = nil
			}
		}
	}

	if err != nil {
		return 0, err
	}

	if len(results) == 0 {
		return 0, fmt.Errorf("anime not found on Jikan")
	}

	matched := MatchAnimeTitle(title, results)
	if matched != nil {
		malIDCache[title] = matched.MalID
		return matched.MalID, nil
	}

	malIDCache[title] = results[0].MalID
	return results[0].MalID, nil
}

// SearchAnime searches Jikan API for an anime and returns a list of results with details.
func SearchAnime(query string) ([]AnimeBasic, error) {
	apiURL := fmt.Sprintf("https://api.jikan.moe/v4/anime?q=%s&order_by=members&sort=desc&limit=15", url.QueryEscape(query))
	
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

	var searchResp AnimeListResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, err
	}

	return searchResp.Data, nil
}

// cacheFilePath returns the path to the filler cache file
func cacheFilePath() string {
	return filepath.Join(helpers.ConfigDir(), "jikan_flags.json")
}

// readCache reads the entire filler cache from disk
func readCache() map[int]map[int]EpisodeFlags {
	cache := make(map[int]map[int]EpisodeFlags)
	data, err := os.ReadFile(cacheFilePath())
	if err == nil {
		json.Unmarshal(data, &cache)
	}
	return cache
}

// writeCache writes the entire filler cache to disk
func writeCache(cache map[int]map[int]EpisodeFlags) {
	os.MkdirAll(helpers.ConfigDir(), 0755)
	data, err := json.MarshalIndent(cache, "", "  ")
	if err == nil {
		os.WriteFile(cacheFilePath(), data, 0644)
	}
}

// GetEpisodeFlags fetches all episodes for a given mal_id and returns a map where the key is the episode number and the value is EpisodeFlags.
func GetEpisodeFlags(malID int) (map[int]EpisodeFlags, error) {
	// First check cache
	cache := readCache()
	if flags, ok := cache[malID]; ok {
		// Apply exceptions even for cached data in case it was cached before the exception was added
		applyExceptions(malID, flags)
		return flags, nil
	}

	flags := make(map[int]EpisodeFlags)
	page := 1
	client := &http.Client{Timeout: 10 * time.Second}

	for {
		apiURL := fmt.Sprintf("https://api.jikan.moe/v4/anime/%d/episodes?page=%d", malID, page)
		resp, err := client.Get(apiURL)
		if err != nil {
			return flags, err // Return what we have so far
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			time.Sleep(1500 * time.Millisecond) // Wait 1.5 seconds and retry this page
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return flags, fmt.Errorf("jikan API error at page %d: %s", page, resp.Status)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return flags, err
		}

		var epResp EpisodesResponse
		if err := json.Unmarshal(body, &epResp); err != nil {
			return flags, err
		}

		for _, ep := range epResp.Data {
			if ep.Filler || ep.Recap {
				flags[ep.MalID] = EpisodeFlags{
					Filler: ep.Filler,
					Recap:  ep.Recap,
				}
			}
		}

		if !epResp.Pagination.HasNextPage {
			break
		}

		page++
		// Respect Jikan's rate limit (safer 400ms delay)
		time.Sleep(400 * time.Millisecond)
	}

	// Apply exceptions
	applyExceptions(malID, flags)

	// Save to cache
	cache[malID] = flags
	writeCache(cache)

	return flags, nil
}

// applyExceptions overrides Jikan's data for known cases
func applyExceptions(malID int, flags map[int]EpisodeFlags) {
	// Global rule: Episode 1 and 2 are usually introductory and shouldn't be skipped, 
	// even if Jikan technically marks them as anime-original filler.
	if flag, ok := flags[1]; ok {
		flag.Filler = false
		flags[1] = flag
	}
	if flag, ok := flags[2]; ok {
		flag.Filler = false
		flags[2] = flag
	}
}
