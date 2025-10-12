<div align="center">

<h1>anitr-cli</h1>
<h3>Terminalde Türkçe altyazılı anime arama ve izleme aracı 🚀</h3>

<img src="https://raw.githubusercontent.com/axrona/anitr-cli/main/assets/anitr-preview.gif" alt="anitr-cli preview" width="600"/>

<p>
  
[![Lisans: GPL3](https://img.shields.io/github/license/axrona/anitr-cli?style=for-the-badge&logo=opensourceinitiative&logoColor=white&label=Lisans)](https://github.com/axrona/anitr-cli/blob/main/LICENSE)
[![Go Versiyon](https://img.shields.io/badge/Go-1.23+-blue?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/dl/)
[![Release](https://img.shields.io/github/v/release/axrona/anitr-cli?style=for-the-badge&logo=github&logoColor=white&label=Son%20Sürüm)](https://github.com/axrona/anitr-cli/releases/latest)
[![AUR](https://img.shields.io/aur/version/anitr-cli?style=for-the-badge&logo=archlinux&logoColor=white&label=AUR)](https://aur.archlinux.org/packages/anitr-cli)
    
</p>

</div>

---

## 🎬 Özellikler

- **Cross-Platform**: Linux, Windows ve macOS üzerinde çalışabilir.
- **AnimeCix ve OpenAnime Entegrasyonu**: Popüler anime platformlarından hızlı arama ve izleme.
- **Fansub Seçimi**: OpenAnime üzerinden izlerken istediğin çeviri grubunu seçebilirsin.
- **Sezon Playlist**: Tüm sezonu otomatik sırayla izlemenizi sağlar, her bölüm arası menüye dönmenize gerek yok!
- **İzleme Geçmişi**: İzlediğin animeler kaydedilir, kaldığın bölümden devam edebilirsin.
- **Arayüz Esnekliği**: Terminal tabanlı TUI ya da minimalist Rofi arayüzünden dilediğini kullan.
- **İndirme Özelliği**: Animeleri indirip internet olmadan da izleme özgürlüğü.
- **Discord Rich Presence**: O an izlediğin animeyi Discord profilinde göster.
- **Otomatik Güncelleme Kontrolü**: Açılışta yeni sürüm varsa otomatik olarak haber verir.

---

## ⚡ Kurulum

### 🐧 Linux

#### Arch tabanlı dağıtımlar (AUR):

```bash
yay -S anitr-cli
```

ya da

```bash
paru -S anitr-cli
```

#### Diğer Linux dağıtımları:

```bash
curl -sS https://raw.githubusercontent.com/axrona/anitr-cli/main/install.sh | bash
```

ya da

```bash
git clone https://github.com/axrona/anitr-cli.git
cd anitr-cli
git fetch --tags
make install-linux
```

> **Gereksinimler:**  
> Derleme: `go`, `git`, `make`  
> Kullanım: `mpv`  
> İsteğe bağlı: `rofi` (Rofi arayüzü için), `youtube-dl`/`yt-dlp` (Bölüm indirme özelliği için)

**Paketleri yüklemek için:**

> [!WARNING]  
> Debian repolarında Go sürümü 1.23'den eski olabilir. Bu yüzden snap ile (`sudo snap install go --classic`) ya da manuel kurulum gerekebilir.

- **Debian/Ubuntu:**
  ```bash
  sudo apt update
  sudo apt install golang git make mpv rofi yt-dlp
  ```
- **Arch/Manjaro:**
  ```bash
  sudo pacman -S go git make mpv rofi yt-dlp
  ```
- **Fedora:**
  ```bash
  sudo dnf install golang git make mpv rofi yt-dlp
  ```
- **openSUSE:**
  ```bash
  sudo zypper install go git make mpv rofi yt-dlp
  ```

### 🪟 Windows

> [!NOTE]
> Windows sürümünde GUI bulunmaz, yalnızca TUI ile çalışır.

1. Sisteminizde [**MPV**](https://sourceforge.net/projects/mpv-player-windows/files/) kurulu olmalıdır.
2. [Releases](https://github.com/axrona/anitr-cli/releases) sayfasından `anitr-cli.exe` indirin.
3. `C:\Program Files\anitr-cli` klasörünü oluşturun.
4. `anitr-cli.exe` dosyasını bu klasöre taşıyın.
5. PATH’e `C:\Program Files\anitr-cli` ekleyin.
6. Anime indirebilmek için [yt-dlp](https://github.com/yt-dlp/yt-dlp/releases/latest) veya [youtube-dl](https://github.com/ytdl-org/youtube-dl/releases) indirin ve PATH'e ekleyin. (Opsiyonel)

Artık **cmd** veya **PowerShell** içinde anitr-cli çalıştırabilirsiniz.

### 💻 MacOS

> [!WARNING]
> Mac cihazım olmadığından dolayı **anitr-cli** MacOS üzerinde test edilmedi.
> Ancak, Linux'ta kullanılan yöntemlerle kurulup çalışması oldukça muhtemeldir. Herhangi bir sorunla karşılaşırsanız lütfen [**issue**](https://github.com/axrona/anitr-cli/issues) açınız.

**Kurulum (Manuel)**:

```bash
git clone https://github.com/axrona/anitr-cli.git
cd anitr-cli
git fetch --tags
make install-macos
```

Anime indirebilmek için [yt-dlp](https://github.com/yt-dlp/yt-dlp/releases/latest) veya [youtube-dl](https://github.com/ytdl-org/youtube-dl/releases) yüklemeniz gerekmektedir:

```bash
brew install yt-dlp
```

ya da

```bash
brew install youtube-dl
```

---

## 🚀 Kullanım

```bash
anitr-cli [alt komut] [bayraklar]
```

```
Bayraklar:
  --disable-rpc       Discord Rich Presence desteğini devre dışı bırakır.
  --go                Son izlenen anime bölümünü açar.
  --version, -v       Sürüm bilgisini gösterir
  --help, -h          Yardım menüsünü gösterir
  --rofi              [Kullanımdan kaldırıldı] Yerine rofi alt komutunu kullanın (Sadece Linux)

Alt komutlar: (Sadece Linux)
  rofi                  Rofi arayüzü ile başlatır
     -f, --rofi-flags      Rofi’ye özel parametreler (örn: --rofi-flags="-theme mytheme")
  tui                   Terminal arayüzü ile başlatır
```

---

## 💡 Sorunlar & Katkı

Her türlü hata, öneri veya katkı için [issue](https://github.com/axrona/anitr-cli/issues) açabilirsiniz. Katkılarınızı bekliyoruz!

---

## 📜 Lisans

Bu proje [GNU GPLv3](https://www.gnu.org/licenses/gpl-3.0.en.html) ile lisanslanmıştır. Detaylar için [LICENSE](LICENSE)
