package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/prayjofir/anitr-cli/internal/utils"
)

// FavoriteEntry tek bir favori kaydı
type FavoriteEntry struct {
	Title   string `json:"title"`
	ID      string `json:"id"`      // anime ID veya slug
	Source  string `json:"source"` // "anizium", "animecix"
	IsMovie bool     `json:"isMovie,omitempty"` // film mi?
	Score   float64  `json:"score,omitempty"`
	Year    int      `json:"year,omitempty"`
	Aired   string   `json:"aired,omitempty"`
	Genres  []string `json:"genres,omitempty"`
}

// favoritesPath — favoriler dosyasının yolu
func favoritesPath() string {
	return filepath.Join(utils.ConfigDir(), "favorites.json")
}

// ReadFavorites tüm favorileri okur.
func ReadFavorites() []FavoriteEntry {
	data, err := os.ReadFile(favoritesPath())
	if err != nil {
		return nil
	}
	var entries []FavoriteEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	return entries
}

// saveFavorites favorileri diske yazar.
func saveFavorites(entries []FavoriteEntry) error {
	_ = os.MkdirAll(filepath.Dir(favoritesPath()), 0755)
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(favoritesPath(), data, 0644)
}

// AddFavorite favoriye anime ekler. Zaten varsa günceller.
func AddFavorite(title, id, source string, isMovie bool, score float64, year int, aired string, genres []string) {
	entries := ReadFavorites()
	// Zaten var mı? Güncelle
	for i, e := range entries {
		if e.ID == id && e.Source == source {
			entries[i].IsMovie = isMovie // güncelle
			entries[i].Score = score
			entries[i].Year = year
			entries[i].Aired = aired
			entries[i].Genres = genres
			_ = saveFavorites(entries)
			return
		}
	}
	// Yoksa yeni ekle
	entries = append(entries, FavoriteEntry{
		Title:   title,
		ID:      id,
		Source:  source,
		IsMovie: isMovie,
		Score:   score,
		Year:    year,
		Aired:   aired,
		Genres:  genres,
	})
	_ = saveFavorites(entries)
}

// RemoveFavorite favoriden anime kaldırır.
func RemoveFavorite(id, source string) {
	entries := ReadFavorites()
	entries = slices.DeleteFunc(entries, func(e FavoriteEntry) bool {
		return e.ID == id && e.Source == source
	})
	_ = saveFavorites(entries)
}

// IsFavorite bir animenin favorilerde olup olmadığını kontrol eder.
func IsFavorite(id, source string) bool {
	for _, e := range ReadFavorites() {
		if e.ID == id && e.Source == source {
			return true
		}
	}
	return false
}

// FavoriteTitles favorilerin başlık listesini döner (menü gösterimi için).
func FavoriteTitles() []string {
	entries := ReadFavorites()
	titles := make([]string, 0, len(entries))
	for _, e := range entries {
		titles = append(titles, e.Title)
	}
	return titles
}
