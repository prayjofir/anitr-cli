# anitr-cli

**anitr-cli**, terminal üzerinden Türkanime ve Anizium kaynaklarını kullanarak anime aramanızı, bölümleri listelemenizi ve doğrudan MPV üzerinden izlemenizi sağlayan hızlı ve modern bir CLI aracıdır.

---

## 🚀 Özellikler

- **Çoklu Kaynak Desteği:** Türkanime ve Anizium üzerinden içerik çekme.
- **TUI/GUI Seçim Menüsü:** `fzf` veya `rofi` araçlarını otomatik algılayarak interaktif menüler oluşturur.
- **MPV Entegrasyonu:** Seçtiğiniz bölümleri doğrudan MPV oynatıcısı üzerinden reklamsız izleme.
- **Bölüm Geçmişi ve Otomatik İlerleme:** İzlenen bölümler kaydedilir; geçmişten seçince bir sonraki bölüm doğrudan başlar.
- **Çoklu Altyazı Desteği:** Tüm altyazı dilleri MPV'ye aynı anda yüklenir, tercih edilen önce gelir.
- **Gelişmiş Anizium Entegrasyonu:** Hesap girişi, profil seçimi, PIN doğrulaması, kalite/ses/altyazı tercihleri.

---

## 🛠️ Kurulum

### Gereksinimler

- [Go](https://go.dev/doc/install) (1.20 veya üzeri)
- [mpv](https://mpv.io/installation/)
- [fzf](https://github.com/junegunn/fzf) veya [rofi](https://github.com/davatorium/rofi)

### Kaynaktan Derleme

```bash
git clone https://github.com/axrona/anitr-cli.git
cd anitr-cli
go build -o anitr
sudo mv anitr /usr/local/bin/
```

---

## 🎮 Kullanım

```bash
anitr
```

### Temel Akış
1. **Kaynak Seçimi:** `Türkanime` veya `Anizium` seçin.
2. **Arama:** İzlemek istediğiniz animenin adını yazın.
3. **Bölüm Seçimi:** Çıkan listeden bölümü seçin.
4. **İzleme:** MPV otomatik açılır.

### Geçmiş Özelliği
- Son 10 izlenen anime geçmişe kaydedilir.
- Geçmişten bir anime seçince **menü gösterilmeden** doğrudan bir sonraki bölüm başlar.
- History: `~/.anitr-cli/history.json`

### Anizium Hesabına Giriş
1. Ana Menü → **"Anizium'a Giriş Yap"**
2. E-posta ve şifrenizi girin.
3. Profilinizi seçin (PIN varsa girin).
4. Bilgiler `~/.config/anitr-cli/anizium.json` dosyasına kaydedilir.

---

## ⚙️ Ayarlar

Ana Menü → **"Ayarlar"** üzerinden yapılandırılabilir:

| Ayar | Seçenekler | Açıklama |
|---|---|---|
| **Tercih Edilen Kalite** | 4K / 2K / 1080p / 720p / 480p / **Sor** | `Sor` seçilirse her izlemede kalite seçim menüsü açılır |
| **Tercih Edilen Ses** | Japonca / Türkçe Dublaj / İngilizce Dublaj / **Sor** | Otomatik ses seçimi veya her seferinde sor |
| **Tercih Edilen Altyazı** | tr / en / de / fr / ... / **Sor** | Tercih edilen dil MPV'de ilk altyazı olur |

---

## 🗂️ Proje Yapısı (Anizium odaklı)

```
internal/
├── sources/anizium/
│   ├── anizium.go       # Ana kaynak; GetWatchData (API tabanlı, CDN probe yok)
│   ├── auth.go          # Login, FetchVideoGroups, FetchSubtitleVTT
│   └── cdn_probe.go     # ⭐ ARŞİV: Hesapsız erişim için CDN probe kodu (ileride kullanılabilir)
├── actions/
│   └── actions.go       # PlayAnimeLoop, autoPlay, çoklu altyazı mantığı
├── cli/
│   └── cli.go           # Ana menü, geçmiş, ayarlar
└── player/
    └── mpv.go           # MPVParams (SubtitleUrls desteği), Play()
```

---

## 📋 Yapılan Değişiklikler (2026-05-04 ~ 05)

### 🔧 Hata Düzeltmeleri

#### Geçmiş Sistemi
- **Kritik:** Anizium için `animeId` history'ye boş string (`""`) kaydediliyordu.
  - **Neden:** `selectedAnimeId` sadece `animecix` için int ID alıyor, `anizium` slug yoluna düşüyordu (Anizium'un slug'ı yok → boş kalıyordu).
  - **Çözüm:** `animecix || anizium` kontrolü eklendi, her ikisi de `strconv.Itoa(selectedAnimeID)` kullanıyor.

#### Geçmişten Oynatma
- **Kritik:** `cfx.AnimeHistory` program başında bir kere yükleniyordu. Kullanıcı bölüm izleyip geçmişe dönünce bellekteki kopya güncellenmemiş oluyordu → 1. bölümden başlıyordu.
  - **Çözüm:** Geçmişten oynatma öncesi `history.ReadAnimeHistory()` ile diskten taze okuma yapılıyor.

#### `goto` Derleme Hatası
- `goto playSwitch`, `menuTitle` değişkeninin deklarasyonunu atlıyordu.
  - **Çözüm:** `goto` kaldırıldı, `if/else` bloğuna dönüştürüldü.

#### `anime data eksik` Hatası
- `GetAnimeByID` hatası geçmiş akışını tamamen durduruyordu.
  - **Çözüm:** `GetAnimeByID` artık fatal değil; başarısız olursa history'deki anime adı fallback olarak kullanılıyor.

---

### ✨ Yeni Özellikler

#### Geçmiş Tabanlı Otomatik Oynatma (Auto-Play)
- Geçmişten anime seçilince `autoPlay: true` ile `PlayAnimeLoop` çağrılıyor.
- Ana menü **gösterilmeden** `lastEpisodeIdx + 1`. bölüm direkt başlıyor.
- Sonraki oturumlarda aynı animeden devam eder.

#### Sezon Ayırıcıları
- `buildSeasonDisplay` helper'ı; bölüm listesine `"────── N. Sezon ──────"` ayırıcıları enjekte ediyor.
- TUI'da `seasonSeparatorItem` ile greyed-out/non-selectable olarak gösteriliyor.
- İndirme multi-select ekranında da aynı ayırıcılar var, seçilemez.

#### 2K (1440p) Kalite Desteği
- Ayarlar → Tercih Edilen Kalite: `"2K"` eklendi.
- Anizium API'den `quality: 1440` → `"2K"` etiketi.
- `preferredStart: "1440p"` filtrelemesi çalışıyor.

#### "Sor" Kalite Modu
- `"Otomatik (En yüksek mevcut)"` → **`"Sor (Manuel seç)"`** olarak değiştirildi.
- `PreferredQuality == ""` (Sor) seçilince:
  - `skip_sound_preference: true` ile API çağrılıyor → tüm kalite+ses kombinasyonları geliyor.
  - Her `İzle` basışında kalite/ses seçim menüsü açılıyor.

#### Çoklu Altyazı MPV Desteği
- `MPVParams.SubtitleUrls []string` eklendi.
- Tüm altyazı dilleri (TR, EN, DE, FR, ES, IT, AR...) aynı anda MPV'ye `--sub-file=URL` olarak geçiliyor.
- **VTT indirme yok** — URL'ler direkt MPV'ye veriliyor.
- Sıralama: `tercih edilen → TR → EN → diğerleri`
- `--slang=tr,en,ja,...` ile MPV varsayılan altyazıyı seçiyor.
- MPV içinde `J` tuşuyla altyazılar arasında geçiş yapılabilir.

---

### ⚡ Performans İyileştirmeleri

#### CDN Probe → Authenticated API (En Büyük Değişiklik)

**Eski sistem:**
```
12 CDN sunucusu × 5 kalite × 3 ses = 180 HEAD isteği
Her istek: 4 saniyelik timeout
Toplam süre: 5-30 saniye
```

**Yeni sistem:**
```
1 API çağrısı: GET /anime/source?id=X&plan=Y&season=Z&episode=W
Yanıt: tüm kaliteler + sesler + altyazılar tek seferde
Süre: ~1 saniye
```

Anizium'un `/anime/source` API endpoint'i yanıtında `groups` alanı bulunduğu keşfedildi:
```json
{
  "groups": [
    { "group": "trdub", "items": [
        { "link": "https://x.aniziumserver.site/95479/1/1/1080p.trdub.mp4", "quality": 1080 }
    ]},
    { "group": "original", "items": [...] }
  ],
  "subtitles": [...],
  "content": { "next_episode_data": {...} }
}
```

**Arşivlenen CDN kodu:** `internal/sources/anizium/cdn_probe.go`
- `ProbeCDNForURL()` — belirli kalite/ses için çalışan URL bulur
- `ProbeCDNAll()` — tüm kombinasyonları tarar
- Hesap gerektirmeyen gelecekteki projeler için korundu
- URL pattern: `https://<server>/<tmdb_id>/<season>/<episode>/<quality>.<sound>.mp4`

---

## 🔮 Sonraki Adımlar (Yarın)

### Planlanan
- [ ] **Hesapsız izleyici projesi** — `cdn_probe.go`'daki pattern kullanılarak hesap gerektirmeyen ayrı bir CLI veya API servisi yapılabilir. TMDB ID'si Anizium search API'sinden (auth gerektirmez) alınabilir.
- [ ] Potansiyel: Anizium `content.next_episode_data` alanından sonraki bölüm linkini direkt kullanmak (şu an API'de mevcut)

### Mevcut Bilinen Eksikler
- `autoPlayTriggered` bayrağı aynı animeden çıkıp geri girilince sıfırlanıyor (istenen davranış) ✅
- `clearMPVCache` fonksiyonu hâlâ mevcut, disk bloat sorunu yok

---

## ⚙️ Teknik Detaylar (Anizium API)

- **Login:** POST `https://api.anizium.co/user/login` — XOR şifreli payload + `Cf-Control` header
- **Profiller:** GET `https://api.anizium.co/user/get` — session token ile
- **Anime Arama:** GET `https://api.anizium.co/page/search?value=X` — auth gerektirmez
- **Anime Detay:** GET `https://api.anizium.co/anime/get?id=X` — auth gerektirmez
- **Video + Altyazı:** GET `https://api.anizium.co/anime/source?id=X&plan=Y&season=Z&episode=W&server=1` — auth gerekli
- **`Cf-Control` Header:** Günün adını içeren XOR şifreli dinamik token
- **Config:** `~/.config/anitr-cli/anizium.json` — email, user_id, token, plan

---

## 🤝 Katkıda Bulunma

Hata bildirimleri ve pull request'ler kabul edilir.

## 📄 Lisans

MIT Lisansı — `LICENSE` dosyasına bakın.
