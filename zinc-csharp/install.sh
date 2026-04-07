#!/bin/bash
# Copyright 2026 ZincScale
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

# zinc-csharp installer — one-stop shop for C# Zinc development
#
# Usage:
#   curl -LsSf https://raw.githubusercontent.com/ZincScale/zinc/master/zinc-csharp/install.sh | bash
#
# Installs:
#   1. .NET 10 SDK (if not present)
#   2. zinc-csharp build tool to ~/.zinc/bin/
#   3. Adds ~/.zinc/bin and ~/.dotnet to PATH (shell rc)
#
# After install: zinc-csharp build, run, test — everything just works.

set -e

ZINC_HOME="$HOME/.zinc"
ZINC_BIN="$ZINC_HOME/bin"
DOTNET_DIR="$HOME/.dotnet"

echo "=== zinc-csharp installer ==="
echo ""

# --- Step 1: Install .NET 10 SDK ---

install_dotnet() {
    if command -v dotnet &>/dev/null; then
        local ver
        ver="$(dotnet --version 2>/dev/null || echo "")"
        if [[ "$ver" == 10.* ]]; then
            echo "[1/3] .NET SDK $ver (already installed)"
            return
        fi
    fi

    if [[ -f "$DOTNET_DIR/dotnet" ]]; then
        local ver
        ver="$("$DOTNET_DIR/dotnet" --version 2>/dev/null || echo "")"
        if [[ "$ver" == 10.* ]]; then
            echo "[1/3] .NET SDK $ver (already at $DOTNET_DIR)"
            return
        fi
    fi

    echo "[1/3] Installing .NET 10 SDK..."
    curl -sSL https://dot.net/v1/dotnet-install.sh -o /tmp/dotnet-install.sh
    chmod +x /tmp/dotnet-install.sh
    /tmp/dotnet-install.sh --channel 10.0 --install-dir "$DOTNET_DIR" 2>&1 | tail -3
    rm -f /tmp/dotnet-install.sh

    local ver
    ver="$("$DOTNET_DIR/dotnet" --version 2>/dev/null || echo "unknown")"
    echo "[1/3] .NET SDK $ver installed"
}

install_dotnet

# --- Step 2: Install zinc-csharp build tool ---

echo "[2/3] Installing zinc-csharp build tool..."
mkdir -p "$ZINC_HOME" "$ZINC_BIN"

# Use local copy if running from clone, otherwise download from GitHub
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd)"
if [[ -f "$SCRIPT_DIR/build-tool/zinc-csharp" ]]; then
    cp "$SCRIPT_DIR/build-tool/zinc-csharp" "$ZINC_BIN/zinc-csharp"
    echo "       copied from local clone"
else
    curl -sSL "https://raw.githubusercontent.com/ZincScale/zinc/master/zinc-csharp/build-tool/zinc-csharp" \
        -o "$ZINC_BIN/zinc-csharp"
    echo "       downloaded from GitHub"
fi
chmod +x "$ZINC_BIN/zinc-csharp"

# --- Step 3: PATH setup ---

setup_path() {
    local dir="$1" label="$2"

    # Already in current PATH?
    if [[ ":$PATH:" == *":$dir:"* ]]; then
        return
    fi

    # Find shell rc file
    local rc=""
    if [[ -f "$HOME/.bashrc" ]]; then rc="$HOME/.bashrc"
    elif [[ -f "$HOME/.zshrc" ]]; then rc="$HOME/.zshrc"
    elif [[ -f "$HOME/.profile" ]]; then rc="$HOME/.profile"
    fi

    if [[ -n "$rc" ]] && ! grep -q "$dir" "$rc" 2>/dev/null; then
        echo "" >> "$rc"
        echo "# $label" >> "$rc"
        echo "export PATH=\"$dir:\$PATH\"" >> "$rc"
    fi

    # Also export for current session
    export PATH="$dir:$PATH"
}

setup_path "$ZINC_BIN" "zinc tools"
setup_path "$DOTNET_DIR" "dotnet SDK"
echo "[3/3] PATH configured"

# --- Verify ---

echo ""
echo "Verifying installation..."
if command -v zinc-csharp &>/dev/null; then
    echo "  zinc-csharp: $(which zinc-csharp)"
else
    echo "  zinc-csharp: $ZINC_BIN/zinc-csharp"
fi

DOTNET_CMD=""
if command -v dotnet &>/dev/null; then
    DOTNET_CMD="$(which dotnet)"
elif [[ -f "$DOTNET_DIR/dotnet" ]]; then
    DOTNET_CMD="$DOTNET_DIR/dotnet"
fi
if [[ -n "$DOTNET_CMD" ]]; then
    echo "  dotnet:      $DOTNET_CMD ($($DOTNET_CMD --version 2>/dev/null))"
fi

echo ""
echo "Done! In a project with zinc.toml:"
echo "  zinc-csharp build    # Native AOT binary"
echo "  zinc-csharp run      # Build + run"
echo "  zinc-csharp test     # Run tests"
echo ""
echo "Note: restart your shell or run:"
echo "  export PATH=\"\$HOME/.zinc/bin:\$HOME/.dotnet:\$PATH\""
