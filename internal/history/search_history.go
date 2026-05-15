package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/prayjofir/anitr-cli/internal/utils"
)

const maxSearchHistory = 15

// SearchHistoryEntry tek bir arama kaydı
type SearchHistoryEntry struct {
	Query string `json:"query"`
}

// searchHistoryPath — arama geçmişi dosyasının yolu
func searchHistoryPath() string {
	return filepath.Join(utils.ConfigDir(), "search_history.json")
}

// ReadSearchHistory arama geçmişini okur. Hata olursa boş liste döner.
func ReadSearchHistory() []string {
	data, err := os.ReadFile(searchHistoryPath())
	if err != nil {
		return nil
	}
	var entries []SearchHistoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	queries := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Query != "" {
			queries = append(queries, e.Query)
		}
	}
	return queries
}

// SaveSearchQuery yeni sorguyu geçmişe ekler (duplicate'i başa taşır, max 15).
func SaveSearchQuery(query string) {
	if query == "" {
		return
	}
	existing := ReadSearchHistory()

	// Varsa kaldır (başa taşıyacağız)
	existing = slices.DeleteFunc(existing, func(s string) bool { return s == query })

	// Başa ekle
	existing = append([]string{query}, existing...)

	// Limit
	if len(existing) > maxSearchHistory {
		existing = existing[:maxSearchHistory]
	}

	entries := make([]SearchHistoryEntry, len(existing))
	for i, q := range existing {
		entries[i] = SearchHistoryEntry{Query: q}
	}

	data, _ := json.MarshalIndent(entries, "", "  ")
	_ = os.MkdirAll(filepath.Dir(searchHistoryPath()), 0755)
	_ = os.WriteFile(searchHistoryPath(), data, 0644)
}

// ClearSearchHistory arama geçmişini temizler.
func ClearSearchHistory() {
	_ = os.Remove(searchHistoryPath())
}
