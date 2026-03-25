#!/bin/sh
# Copyright 2026 victorybhg
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

# Zinc installer — downloads pre-built self-contained package
# Usage: curl -sSL https://raw.githubusercontent.com/ZincScale/zinc/master/install.sh | sh

set -e

REPO="ZincScale/zinc"
INSTALL_DIR="${ZINC_INSTALL_DIR:-$HOME/.local}"

echo "Zinc installer"
echo "=============="

# Detect platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
[ "$ARCH" = "x86_64" ] && ARCH="amd64"
[ "$ARCH" = "aarch64" ] && ARCH="arm64"
[ "$ARCH" = "arm64" ] && ARCH="amd64"  # macOS reports arm64

PLATFORM="${OS}-${ARCH}"
echo "platform: $PLATFORM"

# Get latest release
LATEST=$(curl -sL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": "\(.*\)".*/\1/')
if [ -z "$LATEST" ]; then
    echo "error: could not determine latest release"
    exit 1
fi
echo "version: $LATEST"

# Download
URL="https://github.com/$REPO/releases/download/$LATEST/zinc-$PLATFORM.tar.gz"
echo "downloading: $URL"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -fSL "$URL" -o "$TMPDIR/zinc.tar.gz"
tar xzf "$TMPDIR/zinc.tar.gz" -C "$TMPDIR"

# Install
mkdir -p "$INSTALL_DIR"
rm -rf "$INSTALL_DIR/zinc"
mv "$TMPDIR/zinc" "$INSTALL_DIR/zinc"

# Create symlink in bin
mkdir -p "$INSTALL_DIR/bin"
ln -sf "$INSTALL_DIR/zinc/bin/zinc" "$INSTALL_DIR/bin/zinc"

echo ""
echo "Zinc $LATEST installed to $INSTALL_DIR/zinc"
echo ""
echo "Add to PATH (if not already):"
echo "  export PATH=\"$INSTALL_DIR/bin:\$PATH\""
echo ""
echo "Verify:"
echo "  zinc run hello.zn"
