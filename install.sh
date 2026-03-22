#!/bin/sh
# Copyright 2026 victorybhg
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Zinc installer — installs Zinc + full toolchain (GraalVM, Mill, Quarkus)
# Usage: curl -sSL https://raw.githubusercontent.com/victorybhg/zinc/master/install.sh | sh

set -e

REPO="victorybhg/zinc"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
GITHUB_RELEASES="https://github.com/${REPO}/releases/download"
INSTALL_DIR="${ZINC_INSTALL_DIR:-/usr/local/bin}"
MILL_LAUNCHER_URL="https://raw.githubusercontent.com/com-lihaoyi/mill/main/mill"
GRAALVM_JDK_VERSION="25"
TMPDIR_CLEANUP=""

cleanup() {
    if [ -n "$TMPDIR_CLEANUP" ] && [ -d "$TMPDIR_CLEANUP" ]; then
        rm -rf "$TMPDIR_CLEANUP"
    fi
}

trap cleanup EXIT INT TERM

info()    { printf "\033[1;34m==>\033[0m %s\n" "$1"; }
success() { printf "\033[1;32m==>\033[0m %s\n" "$1"; }
warn()    { printf "\033[1;33m==>\033[0m %s\n" "$1"; }
error()   { printf "\033[1;31merror:\033[0m %s\n" "$1" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Detect environment
# ---------------------------------------------------------------------------

detect_downloader() {
    if command -v curl >/dev/null 2>&1; then
        DOWNLOADER="curl"
    elif command -v wget >/dev/null 2>&1; then
        DOWNLOADER="wget"
    else
        error "Neither curl nor wget found. Please install one of them and try again."
    fi
}

download() {
    url="$1"; dest="$2"
    if [ "$DOWNLOADER" = "curl" ]; then
        curl -fsSL -o "$dest" "$url"
    else
        wget -q -O "$dest" "$url"
    fi
}

download_stdout() {
    url="$1"
    if [ "$DOWNLOADER" = "curl" ]; then
        curl -fsSL "$url"
    else
        wget -q -O- "$url"
    fi
}

detect_os() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "$OS" in
        linux)  OS="linux" ;;
        darwin) OS="darwin" ;;
        *)      error "Unsupported operating system: $OS. Zinc supports linux and darwin (macOS)." ;;
    esac
}

detect_arch() {
    ARCH="$(uname -m)"
    case "$ARCH" in
        x86_64|amd64)   ARCH="amd64" ;;
        aarch64|arm64)   ARCH="arm64" ;;
        *)               error "Unsupported architecture: $ARCH. Zinc supports amd64 and arm64." ;;
    esac
}

has_cmd() { command -v "$1" >/dev/null 2>&1; }

# ---------------------------------------------------------------------------
# 1. Install Zinc binary
# ---------------------------------------------------------------------------

install_zinc() {
    if has_cmd zinc; then
        CURRENT="$(zinc --version 2>/dev/null | head -1)"
        warn "Zinc already installed: $CURRENT (upgrading)"
    fi

    info "Fetching latest Zinc release..."
    VERSION="$(download_stdout "$GITHUB_API" | grep '"tag_name"' | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"
    if [ -z "$VERSION" ]; then
        error "Could not determine the latest release version."
    fi
    VERSION_NUM="${VERSION#v}"
    info "Latest version: $VERSION"

    ARCHIVE="zinc_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
    DOWNLOAD_URL="${GITHUB_RELEASES}/${VERSION}/${ARCHIVE}"

    TMPDIR_CLEANUP="$(mktemp -d)"
    TMPFILE="${TMPDIR_CLEANUP}/${ARCHIVE}"

    info "Downloading ${ARCHIVE}..."
    download "$DOWNLOAD_URL" "$TMPFILE" || error "Download failed. Check releases at: https://github.com/${REPO}/releases"

    info "Extracting..."
    tar -xzf "$TMPFILE" -C "$TMPDIR_CLEANUP"

    BINARY="${TMPDIR_CLEANUP}/zinc"
    if [ ! -f "$BINARY" ]; then
        error "Could not find 'zinc' binary in the extracted archive."
    fi
    chmod +x "$BINARY"

    info "Installing to ${INSTALL_DIR}/zinc..."
    if [ -w "$INSTALL_DIR" ]; then
        mv "$BINARY" "${INSTALL_DIR}/zinc"
    else
        sudo mv "$BINARY" "${INSTALL_DIR}/zinc"
    fi
    success "Zinc $VERSION installed"
}

# ---------------------------------------------------------------------------
# 2. Install GraalVM JDK 25 (includes native-image)
# ---------------------------------------------------------------------------

install_graalvm() {
    # Check if we already have GraalVM JDK 25
    if has_cmd java; then
        JAVA_VER="$(java --version 2>&1 | head -1)"
        case "$JAVA_VER" in
            *GraalVM*25*)
                success "GraalVM JDK 25 already installed: $JAVA_VER"
                return 0
                ;;
        esac
    fi

    info "Installing GraalVM JDK ${GRAALVM_JDK_VERSION}..."

    if [ "$OS" = "darwin" ]; then
        if ! has_cmd brew; then
            error "Homebrew not found. Install it from https://brew.sh and try again."
        fi
        brew install --cask graalvm-jdk 2>/dev/null || brew upgrade --cask graalvm-jdk 2>/dev/null || true
        # macOS may need to remove quarantine
        GRAAL_HOME="$(/usr/libexec/java_home -v ${GRAALVM_JDK_VERSION} 2>/dev/null || true)"
        if [ -n "$GRAAL_HOME" ]; then
            sudo xattr -r -d com.apple.quarantine "$GRAAL_HOME" 2>/dev/null || true
        fi
    else
        # Linux — use SDKMAN
        if ! has_cmd sdk; then
            info "Installing SDKMAN..."
            curl -s "https://get.sdkman.io" | bash
            # Source SDKMAN for this session
            export SDKMAN_DIR="${SDKMAN_DIR:-$HOME/.sdkman}"
            # shellcheck disable=SC1091
            . "$SDKMAN_DIR/bin/sdkman-init.sh"
        fi
        # Find the latest GraalVM CE version for JDK 25
        GRAAL_ID="$(sdk list java 2>/dev/null | grep -oP '\d+\.\d+\.\d+-graalce' | grep "^${GRAALVM_JDK_VERSION}\." | head -1)"
        if [ -z "$GRAAL_ID" ]; then
            error "Could not find GraalVM CE for JDK ${GRAALVM_JDK_VERSION} in SDKMAN."
        fi
        info "Found GraalVM CE: ${GRAAL_ID}"
        sdk install java "$GRAAL_ID" || true
        sdk default java "$GRAAL_ID"
    fi

    # Verify
    if has_cmd java; then
        success "GraalVM JDK installed: $(java --version 2>&1 | head -1)"
    else
        warn "GraalVM JDK installed but 'java' not on PATH. You may need to restart your shell."
    fi

    if has_cmd native-image; then
        success "native-image available: $(native-image --version 2>&1 | head -1)"
    else
        warn "native-image not on PATH. You may need to restart your shell."
    fi
}

# ---------------------------------------------------------------------------
# 3. Install Mill build tool
# ---------------------------------------------------------------------------

install_mill() {
    if has_cmd mill; then
        success "Mill already installed: $(mill --version 2>/dev/null | head -1)"
        return 0
    fi

    info "Installing Mill (launcher script — auto-downloads correct version)..."

    MILL_BIN="${INSTALL_DIR}/mill"
    MILL_TMP="$(mktemp)"
    download "$MILL_LAUNCHER_URL" "$MILL_TMP"
    chmod +x "$MILL_TMP"
    if [ -w "$INSTALL_DIR" ]; then
        mv "$MILL_TMP" "$MILL_BIN"
    else
        sudo mv "$MILL_TMP" "$MILL_BIN"
    fi

    if has_cmd mill; then
        success "Mill installed: $(mill --version 2>/dev/null | head -1)"
    else
        warn "Mill installed but not on PATH. Ensure ${INSTALL_DIR} is in your \$PATH."
    fi
}

# ---------------------------------------------------------------------------
# 4. Install Quarkus CLI
# ---------------------------------------------------------------------------

install_quarkus() {
    if has_cmd quarkus; then
        success "Quarkus CLI already installed: $(quarkus --version 2>/dev/null)"
        return 0
    fi

    info "Installing Quarkus CLI..."

    if [ "$OS" = "darwin" ] && has_cmd brew; then
        brew install quarkus 2>/dev/null || true
    else
        # Linux — use SDKMAN
        if ! has_cmd sdk; then
            export SDKMAN_DIR="${SDKMAN_DIR:-$HOME/.sdkman}"
            if [ -f "$SDKMAN_DIR/bin/sdkman-init.sh" ]; then
                # shellcheck disable=SC1091
                . "$SDKMAN_DIR/bin/sdkman-init.sh"
            else
                error "SDKMAN not found. It should have been installed with GraalVM."
            fi
        fi
        sdk install quarkus || true
    fi

    if has_cmd quarkus; then
        success "Quarkus CLI installed: $(quarkus --version 2>/dev/null)"
    else
        warn "Quarkus CLI installed but not on PATH. You may need to restart your shell."
    fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    echo ""
    info "Zinc Toolchain Installer"
    echo ""
    echo "  This will install:"
    echo "    1. Zinc          — the Zinc compiler CLI"
    echo "    2. GraalVM JDK 25 — Java runtime + native-image (AOT compiler)"
    echo "    3. Mill          — build tool for projects with dependencies"
    echo "    4. Quarkus CLI   — web services, REST APIs, dev mode"
    echo ""

    detect_downloader
    detect_os
    detect_arch

    echo "  Platform: ${OS}/${ARCH}"
    echo ""

    install_zinc
    echo ""
    install_graalvm
    echo ""
    install_mill
    echo ""
    install_quarkus

    echo ""
    echo "---------------------------------------------------------------"
    success "Zinc toolchain installed!"
    echo ""
    echo "  Verify:"
    echo "    zinc --version"
    echo "    java --version"
    echo "    native-image --version"
    echo "    mill --version"
    echo "    quarkus --version"
    echo ""
    echo "  Get started:"
    echo "    zinc init myapp && cd myapp && zinc run src/main.zn"
    echo ""
    echo "  If commands aren't found, restart your shell or run:"
    if [ "$OS" = "darwin" ]; then
        echo "    source ~/.zshrc"
    else
        echo "    source ~/.bashrc"
    fi
    echo "---------------------------------------------------------------"
    echo ""
}

main
