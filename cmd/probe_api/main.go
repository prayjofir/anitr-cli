package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AniziumConfig struct {
	Email  string `json:"email"`
	UserID string `json:"user_id"`
	Token  string `json:"token"`
	Plan   string `json:"plan"`
}

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
	letters := "abcdefghijklmnopqrstuvwxyz0123456789"
	s := make([]byte, n)
	for i := range s {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		s[i] = letters[idx.Int64()]
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

func main() {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".config", "anitr-cli", "anizium.json"))
	if err != nil {
		fmt.Println("Config yok:", err)
		os.Exit(1)
	}
	var cfg AniziumConfig
	json.Unmarshal(data, &cfg)
	fmt.Printf("UserID: %s | Plan: %s\n\n", cfg.UserID, cfg.Plan)

	animeID := "98803"
	season  := "1"
	episode := "1"
	if len(os.Args) > 1 { animeID = os.Args[1] }
	if len(os.Args) > 2 { season  = os.Args[2] }
	if len(os.Args) > 3 { episode = os.Args[3] }

	plan := cfg.Plan
	if plan == "" { plan = "standart" }

	apiURL := fmt.Sprintf(
		"https://api.anizium.co/anime/source?id=%s&site=main&plan=%s&season=%s&episode=%s&server=1",
		animeID, plan, season, episode,
	)
	fmt.Println("URL:", apiURL)

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Cf-Control", generateCfToken())
	req.Header.Set("device", "browser")
	req.Header.Set("language", "tr")
	req.Header.Set("site", "main")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("user-session", cfg.Token)
	req.Header.Set("user", cfg.UserID)

	fmt.Println("Token ilk 20 char:", cfg.Token[:20])
	fmt.Println("Token hex decode:", func() string {
		b, err := hex.DecodeString(cfg.Token)
		if err != nil { return "(hex değil)" }
		return string(b)
	}())

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Bağlantı hatası:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("\n--- HAM YANIT (HTTP %d) ---\n", resp.StatusCode)
	var pretty interface{}
	if json.Unmarshal(body, &pretty) == nil {
		out, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Println(string(body))
	}
}
