// models paketi, anime verilerini ve ilgili yapılarını tanımlar.
package models

import (
	"log"
	"os"
	"time"
)

// Uygulama durumu ve ayarlarını saklayan struct
type App struct {
	Source         *AnimeSource
	SelectedSource *string
	UiMode         *string
	RofiFlags      *string
	DisableRPC     *bool
	AnimeHistory   *AnimeHistory
	HistoryLimit   int
	Logger         *LogServ
}

// AnimeSource arayüzü, farklı anime kaynaklarından veri çekme işlevlerini tanımlar.
type AnimeSource interface {
	// Arama sorgusuna göre anime verilerini getirir.
	GetSearchData(query string) ([]Anime, error)
	// Id/Slug ile anime verisini getirir.
	GetAnimeByID(id string) (*Anime, error)
	// Sezon verilerini getirir.
	GetSeasonsData(params SeasonParams) ([]Season, error)
	// Bölüm verilerini getirir.
	GetEpisodesData(params EpisodeParams) ([]Episode, error)
	// İzleme verilerini getirir.
	GetWatchData(params WatchParams) ([]Watch, error)
	// Kaynağın adını döner.
	Source() string
}

type AppActions interface {
	MainMenu(cfx *App, timestamp time.Time)
	PlayAnimeLoop(
		source AnimeSource, // Seçilen anime kaynağı (OpenAnime, AnimeciX)
		SelectedSource string, // Seçilen kaynak ismi
		episodes []Episode, // Tüm bölümler
		episodeNames []string, // Bölüm adları
		selectedAnimeID int, // Seçilen anime ID'si (AnimeciX için)
		selectedAnimeSlug string, // Seçilen anime slug'ı (OpenAnime için)
		selectedAnimeName string, // Seçilen anime ismi
		isMovie bool, // Film mi yoksa dizi mi olduğunu belirtir
		selectedSeasonIndex int, // Seçilen sezonun index'i
		UiMode string, // Arayüz tipi (örneğin terminal, rofi, vs.)
		RofiFlags string, // Rofi için özel bayraklar
		posterURL string, // Poster görseli URL'si (Discord RPC için)
		DisableRPC bool, // Discord RPC devre dışı mı?
		timestamp time.Time, // Discord RPC timestamp
		AnimeHistory AnimeHistory, // Geçmiş veri tipi
		Logger *LogServ, // Logger
	) (AnimeSource, string, error)
}

// LogServ, hata ve mesajları bir dosyaya yazmak için yapılandırılmış bir log yapısıdır.
type LogServ struct {
	File *os.File    // Log dosyasının kendisi
	Log  *log.Logger // Log işlemini gerçekleştiren nesne
}

// AnimeHistoryEntry, her anime için tutulacak bilgiler
type AnimeHistoryEntry struct {
	LastEpisodeIdx    *int       `json:"lastEpisodeIdx"`
	LastEpisodeName   string     `json:"lastEpisodeName"`
	AnimeId           *string    `json:"animeId"`
	LastWatched       *time.Time `json:"lastWatched"`
	LastPositionSec   *float64   `json:"lastPositionSec,omitempty"`   // Kaldığı saniye (bitmişse nil)
	IsFinished        bool       `json:"isFinished,omitempty"`         // Bölüm %90+ izlendiyse true
	TotalEpisodeCount int        `json:"totalEpisodeCount,omitempty"` // Son izlemede toplam bölüm sayısı
	IsMovie           bool       `json:"isMovie,omitempty"`           // Film mi? (bölüm yükleme için)
}

// AnimeHistory, source -> anime adı -> struct
type AnimeHistory map[string]map[string]AnimeHistoryEntry

// Anime yapısı, bir anime hakkında temel bilgileri içerir.
type Anime struct {
	Title     string                 // Anime başlığı
	ID        *int                   // Anime ID'si (nullable)
	Slug      *string                // URL dostu ad (nullable)
	Type      *string                // Anime türü (dizi/film vb.)
	TitleType *string                // Başlık türü (anime, film vb.)
	ImageURL  string                 // Anime'nin görseli için URL
	Source    string                 // Kaynağın adı
	Extra     map[string]interface{} // Ekstra veri (her türlü bilgi için esnek alan)
}

// Season yapısı, bir anime'nin sezon bilgilerini içerir.
type Season struct {
	Seasons *[]int  // Sezon numaraları (örneğin 1, 2, 3 gibi)
	Count   *int    // Toplam sezon sayısı
	Type    *string // Sezon tipi (normal, movie vb.)
	IsMovie *bool   // Sezonun bir film olup olmadığı
}

// Episode yapısı, bir anime bölümünün bilgilerini içerir.
type Episode struct {
	ID     string                 // Bölüm ID'si
	Title  string                 // Bölüm başlığı
	Number int                    // Bölüm numarası
	Extra  map[string]interface{} // Ekstra veriler
}

// Fansub yapısı, bir anime için Türkçe altyazı ekleyen grup hakkında bilgileri içerir.
type Fansub struct {
	ID         *string // Fansub ID'si (nullable)
	Name       *string // Fansub adı (nullable)
	SecureName *string // Fansub güvenli adı (nullable)
}

// WatchParams yapısı, izleme işlemi için gerekli parametreleri içerir.
type WatchParams struct {
	Slug    *string                 // Anime URL dostu adı (nullable)
	Url     *string                 // İzleme URL'si (nullable)
	Id      *int                    // Anime ID'si (nullable)
	IsMovie *bool                   // Anime'nin film olup olmadığı (nullable)
	Extra   *map[string]interface{} // Ekstra bilgiler (nullable)
}

// FansubParams yapısı, bir bölüm için fansub verilerini filtrelemek amacıyla parametreleri içerir.
type FansubParams struct {
	Slug       *string // Anime URL dostu adı (nullable)
	Id         *int    // Anime ID'si (nullable)
	SeasonNum  *int    // Sezon numarası (nullable)
	EpisodeNum *int    // Bölüm numarası (nullable)
}

// SeasonParams yapısı, bir anime sezonu için gerekli parametreleri içerir.
type SeasonParams struct {
	Slug *string // Anime URL dostu adı (nullable)
	Id   *int    // Anime ID'si (nullable)
}

// EpisodeParams yapısı, bir anime bölümü için gerekli parametreleri içerir.
type EpisodeParams struct {
	Slug     *string // Anime URL dostu adı (nullable)
	SeasonID *int    // Sezon ID'si (nullable)
}

// WatchSubtitle, bir altyazı seçeneğini temsil eder.
type WatchSubtitle struct {
	Group string // dil kodu: "tr", "en" vb.
	Label string // görünen ad: "Türkçe", "İngilizce" vb.
	Link  string // ham VTT URL'si (henüz indirilmedi)
}

// NextEpisodeData, API'nin döndürdüğü bir sonraki bölüm bilgisini temsil eder.
type NextEpisodeData struct {
	Season  int // Bir sonraki bölümün sezonu
	Episode int // Bir sonraki bölümün numarası
}

// OpeningData, bir bölümün intro/opening zaman aralığını temsil eder.
type OpeningData struct {
	Start string // "00:01:01"
	End   string // "00:02:31"
}

// Watch yapısı, bir anime'yi izlerken gerekli olan izleme bilgilerini içerir.
type Watch struct {
	Labels      []string        // Etiketler ("1080", "720p", vb.)
	Urls        []string        // İzleme URL'leri
	TRCaption   *string         // Varsayılan altyazı yerel dosya yolu (nullable)
	Subtitles   []WatchSubtitle // Tüm altyazı seçenekleri (ham linkler)
	WarnMessage string          // Ses/kalite fallback uyarı mesajı (boşsa gösterilmez)
	NextEpisode *NextEpisodeData // API'den gelen sonraki bölüm bilgisi (nil ise son bölüm)
	Opening     *OpeningData    // Intro/opening zaman aralığı (MPV chapter için)
	Ending      *OpeningData    // Ending/outro zaman aralığı (MPV chapter için)
}
