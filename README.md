# anitr-cli

**anitr-cli**, terminal üzerinden anime aramanızı, bölümleri listelemenizi ve doğrudan MPV üzerinden izlemenizi sağlayan hızlı ve modern bir CLI aracıdır.

---

## 🚀 Özellikler

- **Çoklu Kaynak Desteği:** AnimeciX, Anizium ve Anizium Free üzerinden içerik çekme.
- **Anizium Free:** Hesap gerektirmeden Anizium içeriklerini izle — CDN paralel tarama ile otomatik kaynak bulma.
- **MyAnimeList (Jikan) Entegrasyonu:** Animelerin puan, yayınlanma yılı ve tür gibi bilgilerini MAL üzerinden çeker. Uzun isimli seriler için akıllı fallback arama özelliği içerir.
- **TUI/GUI Seçim Menüsü:** `fzf` veya `rofi` araçlarını otomatik algılayarak interaktif menüler oluşturur.
- **MPV Entegrasyonu:** Seçtiğiniz bölümleri doğrudan MPV oynatıcısı üzerinden reklamsız izleme.
- **Saniye Bazlı Geçmiş ve Devam:** İzlenen bölümler ve konum kaydedilir; geçmişten seçince kaldığın yerden devam eder.
- **Çoklu Altyazı Desteği:** Tüm altyazı dilleri MPV'ye aynı anda yüklenir, tercih edilen önce gelir. MPV içinde `J` tuşuyla değiştirilebilir.
- **Opening Skip:** `A` tuşuyla opening'i anında atla.
- **Arama Geçmişi:** Son 15 arama sorgusu hatırlanır.
- **Favoriler:** Anime kaynak bazlı favori listesi.
- **Discord Rich Presence:** İzlediğin animeyi Discord'da göster.
- **Otomatik Altyazı Sunucu Keşfi:** Anizium Free için aktif altyazı sunucuları arka planda taranır ve cache'lenir.
- **Gelişmiş Anizium Entegrasyonu:** Hesap girişi, profil seçimi, PIN doğrulaması, kalite/ses/altyazı tercihleri.

---

## 🛠️ Kurulum

### Gereksinimler

- [mpv](https://mpv.io/installation/)
- [fzf](https://github.com/junegunn/fzf) veya [rofi](https://github.com/davatorium/rofi)
- `yt-dlp` (opsiyonel, indirme özelliği için)

### AUR (Arch / CachyOS / Manjaro)

```bash
yay -S anitr-cli-git
# veya
paru -S anitr-cli-git
```

### Kaynaktan Derleme

```bash
git clone https://github.com/prayjofir/anitr-cli.git
cd anitr-cli
go build -o anitr .
sudo mv anitr /usr/local/bin/anitr-cli
```

> Go 1.20 veya üzeri gereklidir: [go.dev/doc/install](https://go.dev/doc/install)

---

## 🎮 Kullanım

```bash
anitr-cli
```

### Temel Akış

1. **Kaynak Seçimi:** `AnimeciX`, `Anizium` veya `Anizium Free` seçin.
2. **Arama:** İzlemek istediğiniz animenin adını yazın.
3. **Bölüm Seçimi:** Çıkan listeden bölümü seçin.
4. **İzleme:** MPV otomatik açılır.

### Kaynaklar Hakkında

| Kaynak | Hesap | Özellik |
|--------|-------|---------|
| **AnimeciX** | ❌ Gerekmez | Türkçe altyazılı anime |
| **Anizium** | ✅ Gerekir | 4K/2K kalite, çoklu dil altyazı, Türkçe dublaj |
| **Anizium Free** | ❌ Gerekmez | Anizium içerikleri hesapsız, TR altyazı ile |

### Geçmiş

- Son 10 izlenen anime geçmişe kaydedilir (kaldığın saniyeyle birlikte).
- Geçmişten seçince **kaldığın yerden** devam eder.
- Bölüm %90 izlenince "tamamlandı" olarak işaretlenir.
- Geçmiş dosyası: `~/.config/anitr-cli/history.json`

### Anizium Hesabına Giriş

1. Ana Menü → **"Anizium'a Giriş Yap"**
2. E-posta ve şifrenizi girin.
3. Profilinizi seçin (PIN varsa girin).
4. Bilgiler `~/.config/anitr-cli/anizium.json` dosyasına kaydedilir.

---

## ⚙️ Ayarlar

Ana Menü → **"Ayarlar"** üzerinden yapılandırılabilir:

| Ayar | Seçenekler | Açıklama |
|------|-----------|----------|
| **Tercih Edilen Kalite** | 4K / 2K / 1080p / 720p / 480p / **Sor** | `Sor` seçilirse her izlemede kalite seçim menüsü açılır |
| **Tercih Edilen Ses** | Japonca / Türkçe Dublaj / İngilizce Dublaj / **Sor** | Otomatik ses seçimi veya her seferinde sor |
| **Tercih Edilen Altyazı** | tr / en / de / fr / ... | Tercih edilen dil MPV'de ilk altyazı olur |
| **MAL Kullanıcı Adı** | Kullanıcı Adınız | Jikan API (MyAnimeList) özellikleri için hesabınızı bağlayın |
| **İndirme Dizini** | Yol | İndirilen bölümlerin kaydedileceği klasör |

Ayarlar `~/.config/anitr-cli/config.json` dosyasına kaydedilir.

---

## 📁 Config Dosyaları

| Dosya | İçerik |
|-------|--------|
| `~/.config/anitr-cli/config.json` | Uygulama ayarları |
| `~/.config/anitr-cli/anizium.json` | Anizium hesap bilgileri |
| `~/.config/anitr-cli/history.json` | İzleme geçmişi |
| `~/.config/anitr-cli/search_history.json` | Arama geçmişi (son 15) |
| `~/.config/anitr-cli/favorites.json` | Favori listesi |
| `~/.config/anitr-cli/subtitle_servers.json` | Anizium Free altyazı sunucu cache |

---

## 🤝 Katkıda Bulunma

Hata bildirimleri ve pull request'ler kabul edilir.

## 📄 Lisans

GPL-3.0 Lisansı — `LICENSE` dosyasına bakın.
