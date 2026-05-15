#!/usr/bin/env bash
set -euo pipefail

REPO_URL="https://github.com/prayjofir/anitr-cli"
INSTALL_DIR="/usr/bin"
CLONE_DIR="$(mktemp -d)"
BINARY_NAME="anitr-cli"

echo -e "\n🚀 anitr-cli kurulumu başlıyor...\n"

# Gerekli araçları kontrol et
for cmd in git go; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "❌ '$cmd' yüklü değil, lütfen kurun."
        exit 1
    fi
done

# Make yüklü mü
HAS_MAKE=0
if command -v make &>/dev/null && [[ -f Makefile ]]; then
    HAS_MAKE=1
fi

echo "📥 Repo klonlanıyor..."
git clone "$REPO_URL" "$CLONE_DIR" &>/dev/null
cd "$CLONE_DIR"

# En son tag'a geçiş yapmaya çalış
if git fetch --tags &>/dev/null && git describe --tags --abbrev=0 &>/dev/null; then
    LATEST_TAG=$(git describe --tags --abbrev=0)
    echo "🔖 Sürüm: $LATEST_TAG"
    git checkout "$LATEST_TAG" &>/dev/null
else
    echo "⚠️ Tag bulunamadı, 'main' dalı kullanılacak."
fi

echo "🧹 Go modülleri düzenleniyor ve kod formatlanıyor..."
export GOFLAGS="-mod=mod"
go mod tidy
go fmt ./...

echo "⚙️ Derleniyor ve kuruluyor..."

if [[ "$HAS_MAKE" -eq 1 ]]; then
    if [[ $EUID -ne 0 ]]; then
        sudo make install-linux &>/dev/null
    else
        make install-linux &>/dev/null
    fi
else
    VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "dev")
    BUILDENV=$(go version || echo "linux")
    go build -o "$BINARY_NAME" -ldflags="-X 'github.com/prayjofir/anitr-cli/internal/update.version=$VERSION' -X 'github.com/prayjofir/anitr-cli/internal/update.buildEnv=$BUILDENV'"
    if [[ $EUID -ne 0 ]]; then
        sudo install -Dm755 "$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
    else
        install -Dm755 "$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
    fi
fi

echo -e "\n✅ Kurulum başarılı: $INSTALL_DIR/$BINARY_NAME"

echo -n "📌 Versiyon: "
"$INSTALL_DIR/$BINARY_NAME" --version || echo "Bilgi alınamadı."

echo -e "\n🧹 Geçici dosyalar temizleniyor..."
rm -rf "$CLONE_DIR"

echo "🎉 Kurulum tamamlandı.\n"
