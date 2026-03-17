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

# Zinc installer — https://github.com/victorybhg/zinc
# Usage: curl -sSL https://raw.githubusercontent.com/victorybhg/zinc/master/install.sh | sh

set -e

REPO="victorybhg/zinc"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
GITHUB_RELEASES="https://github.com/${REPO}/releases/download"
INSTALL_DIR="${ZINC_INSTALL_DIR:-/usr/local/bin}"
TMPDIR_CLEANUP=""

cleanup() {
    if [ -n "$TMPDIR_CLEANUP" ] && [ -d "$TMPDIR_CLEANUP" ]; then
        rm -rf "$TMPDIR_CLEANUP"
    fi
}

trap cleanup EXIT INT TERM

info() {
    printf "\033[1;34m==>\033[0m %s\n" "$1"
}

error() {
    printf "\033[1;31merror:\033[0m %s\n" "$1" >&2
    exit 1
}

# Detect a download command (curl or wget)
detect_downloader() {
    if command -v curl >/dev/null 2>&1; then
        DOWNLOADER="curl"
    elif command -v wget >/dev/null 2>&1; then
        DOWNLOADER="wget"
    else
        error "Neither curl nor wget found. Please install one of them and try again."
    fi
}

# Download a URL to a file
download() {
    url="$1"
    dest="$2"
    if [ "$DOWNLOADER" = "curl" ]; then
        curl -fsSL -o "$dest" "$url"
    else
        wget -q -O "$dest" "$url"
    fi
}

# Download a URL and print to stdout
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

get_latest_version() {
    info "Fetching latest release version..."
    VERSION="$(download_stdout "$GITHUB_API" | grep '"tag_name"' | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"
    if [ -z "$VERSION" ]; then
        error "Could not determine the latest release version. Check your internet connection or try again later."
    fi
    # Strip leading 'v' for archive naming
    VERSION_NUM="${VERSION#v}"
    info "Latest version: $VERSION"
}

main() {
    info "Zinc Installer"
    echo ""

    detect_downloader
    detect_os
    detect_arch
    get_latest_version

    # Construct archive name
    if [ "$OS" = "windows" ]; then
        ARCHIVE="zinc_${VERSION_NUM}_${OS}_${ARCH}.zip"
    else
        ARCHIVE="zinc_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
    fi
    DOWNLOAD_URL="${GITHUB_RELEASES}/${VERSION}/${ARCHIVE}"

    # Create temp directory
    TMPDIR_CLEANUP="$(mktemp -d)"
    TMPFILE="${TMPDIR_CLEANUP}/${ARCHIVE}"

    info "Downloading ${ARCHIVE}..."
    download "$DOWNLOAD_URL" "$TMPFILE" || error "Download failed. The release archive may not exist for your platform (${OS}/${ARCH}).\nCheck available releases at: https://github.com/${REPO}/releases"

    info "Extracting..."
    if [ "$OS" = "windows" ]; then
        unzip -q -o "$TMPFILE" -d "$TMPDIR_CLEANUP"
    else
        tar -xzf "$TMPFILE" -C "$TMPDIR_CLEANUP"
    fi

    # Find the binary
    BINARY="${TMPDIR_CLEANUP}/zinc"
    if [ ! -f "$BINARY" ]; then
        error "Could not find 'zinc' binary in the extracted archive."
    fi
    chmod +x "$BINARY"

    # Install
    info "Installing to ${INSTALL_DIR}/zinc..."
    if [ -w "$INSTALL_DIR" ]; then
        mv "$BINARY" "${INSTALL_DIR}/zinc"
    else
        info "Elevated permissions required to install to ${INSTALL_DIR}"
        sudo mv "$BINARY" "${INSTALL_DIR}/zinc"
    fi

    echo ""
    printf "\033[1;32mZinc %s installed successfully!\033[0m\n" "$VERSION"
    echo ""
    echo "  Binary: ${INSTALL_DIR}/zinc"
    echo ""
    echo "Make sure ${INSTALL_DIR} is in your \$PATH, then run:"
    echo ""
    echo "  zinc --version"
    echo ""
    echo "Prerequisites for building Zinc projects:"
    echo "  .NET 10+ SDK  — https://dotnet.microsoft.com/download"
    echo ""
}

main
