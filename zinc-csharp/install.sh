#!/bin/bash
# Copyright 2026 ZincScale
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

# zinc-csharp installer — installs .NET 10 SDK + zinc-csharp build tool
#
# Usage:
#   curl -LsSf https://raw.githubusercontent.com/ZincScale/zinc/master/zinc-csharp/install.sh | bash
#
# Installs:
#   - .NET 10 SDK (if not present)
#   - zinc-csharp build tool
#
# Everything goes into ~/.zinc/ and ~/.dotnet/

set -e

ZINC_HOME="$HOME/.zinc"
ZINC_BIN="$ZINC_HOME/bin"
DOTNET_DIR="$HOME/.dotnet"

echo "Installing zinc-csharp..."

# --- Step 1: Install .NET 10 SDK ---

install_dotnet() {
    if command -v dotnet &>/dev/null; then
        local ver
        ver="$(dotnet --version 2>/dev/null || echo "")"
        if [[ "$ver" == 10.* ]]; then
            echo "  .NET SDK: $ver (already installed)"
            return
        fi
    fi

    if [[ -f "$DOTNET_DIR/dotnet" ]]; then
        local ver
        ver="$("$DOTNET_DIR/dotnet" --version 2>/dev/null || echo "")"
        if [[ "$ver" == 10.* ]]; then
            echo "  .NET SDK: $ver (already installed at $DOTNET_DIR)"
            return
        fi
    fi

    echo "  installing .NET 10 SDK..."
    curl -sSL https://dot.net/v1/dotnet-install.sh -o /tmp/dotnet-install.sh
    chmod +x /tmp/dotnet-install.sh
    /tmp/dotnet-install.sh --channel 10.0 --install-dir "$DOTNET_DIR" 2>&1 | tail -3
    rm -f /tmp/dotnet-install.sh

    local ver
    ver="$("$DOTNET_DIR/dotnet" --version 2>/dev/null || echo "unknown")"
    echo "  .NET SDK: $ver"
}

install_dotnet

# --- Step 2: Install zinc-csharp build tool ---

echo "  installing zinc-csharp build tool..."
mkdir -p "$ZINC_HOME" "$ZINC_BIN"

# Download from repo (or use local if running from clone)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd)"
if [[ -f "$SCRIPT_DIR/build-tool/zinc-csharp" ]]; then
    cp "$SCRIPT_DIR/build-tool/zinc-csharp" "$ZINC_BIN/zinc-csharp"
else
    # Download from GitHub
    curl -sSL "https://raw.githubusercontent.com/ZincScale/zinc/master/zinc-csharp/build-tool/zinc-csharp" \
        -o "$ZINC_BIN/zinc-csharp"
fi
chmod +x "$ZINC_BIN/zinc-csharp"
echo "  zinc-csharp: $ZINC_BIN/zinc-csharp"

# --- Step 3: Ensure PATH ---

add_to_path() {
    local dir="$1" label="$2"
    SHELL_RC=""
    if [ -f "$HOME/.bashrc" ]; then SHELL_RC="$HOME/.bashrc"
    elif [ -f "$HOME/.zshrc" ]; then SHELL_RC="$HOME/.zshrc"
    elif [ -f "$HOME/.profile" ]; then SHELL_RC="$HOME/.profile"
    fi

    if [ -n "$SHELL_RC" ] && ! grep -q "$dir" "$SHELL_RC" 2>/dev/null; then
        echo "" >> "$SHELL_RC"
        echo "# $label" >> "$SHELL_RC"
        echo "export PATH=\"$dir:\$PATH\"" >> "$SHELL_RC"
        echo "  added $dir to PATH in $SHELL_RC"
    fi
}

add_to_path "$ZINC_BIN" "zinc"
add_to_path "$DOTNET_DIR" "dotnet"

echo ""
echo "zinc-csharp installed!"
echo ""
echo "  To start using zinc-csharp, run:"
echo "    export PATH=\"\$HOME/.zinc/bin:\$HOME/.dotnet:\$PATH\""
echo ""
echo "  Then in a project with zinc.toml:"
echo "    zinc-csharp build    # Native AOT binary"
echo "    zinc-csharp run      # Build + run"
echo "    zinc-csharp test     # Run tests"
