#!/bin/bash
# Zinc installer — downloads prebuilt binary or builds from source
set -e

REPO="ZincScale/zinc"
INSTALL_DIR="${ZINC_INSTALL_DIR:-$HOME/.zinc/bin}"
VERSION="${ZINC_VERSION:-latest}"

# Detect platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
esac

ensure_path() {
    if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
        SHELL_RC=""
        if [ -n "$ZSH_VERSION" ]; then
            SHELL_RC="$HOME/.zshrc"
        elif [ -n "$BASH_VERSION" ]; then
            SHELL_RC="$HOME/.bashrc"
        fi
        if [ -n "$SHELL_RC" ]; then
            echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$SHELL_RC"
            echo "Added $INSTALL_DIR to PATH in $SHELL_RC"
            echo "Run: source $SHELL_RC"
        else
            echo "Add to your PATH: export PATH=\"$INSTALL_DIR:\$PATH\""
        fi
    fi
}

echo "Installing Zinc ($OS/$ARCH)..."
mkdir -p "$INSTALL_DIR"

# Try downloading prebuilt binary from GitHub releases
if command -v curl &>/dev/null; then
    if [ "$VERSION" = "latest" ]; then
        DOWNLOAD_URL=$(curl -sL "https://api.github.com/repos/$REPO/releases/latest" \
            | grep "browser_download_url.*zinc_.*_${OS}_${ARCH}.tar.gz" \
            | head -1 | cut -d '"' -f 4)
    else
        DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/zinc_${VERSION#v}_${OS}_${ARCH}.tar.gz"
    fi

    if [ -n "$DOWNLOAD_URL" ]; then
        echo "Downloading from $DOWNLOAD_URL"
        TMP=$(mktemp -d)
        curl -sL "$DOWNLOAD_URL" | tar xz -C "$TMP"
        mv "$TMP/zinc" "$INSTALL_DIR/zinc"
        chmod +x "$INSTALL_DIR/zinc"
        rm -rf "$TMP"

        echo "Installed zinc to $INSTALL_DIR/zinc"
        ensure_path
        "$INSTALL_DIR/zinc" version
        echo ""
        echo "Done! Try: zinc init myapp && cd myapp && zinc run"
        exit 0
    fi
fi

# Fallback: build from source
echo "No prebuilt binary found — building from source..."
if ! command -v go &>/dev/null; then
    echo "Error: Go is required to build from source."
    echo "Install Go 1.26+: https://go.dev/dl/"
    exit 1
fi

TMP=$(mktemp -d)
git clone --depth 1 "https://github.com/$REPO.git" "$TMP/zinc"
cd "$TMP/zinc/zinc-go"
go build -ldflags "-s -w" -o "$INSTALL_DIR/zinc" ./cmd/zinc/
rm -rf "$TMP"

echo "Built and installed zinc to $INSTALL_DIR/zinc"
ensure_path
"$INSTALL_DIR/zinc" version
echo ""
echo "Done! Try: zinc init myapp && cd myapp && zinc run"
