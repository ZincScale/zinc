#!/bin/bash
# Copyright 2026 CaravanScale
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

# caravan-csharp installer — one-stop shop for C# Caravan development
#
# Usage:
#   curl -LsSf https://raw.githubusercontent.com/CaravanScale/caravan/master/caravan-csharp/install.sh | bash
#
# Installs:
#   1. .NET 10 SDK (if not present)
#   2. caravan-csharp build tool to ~/.caravan/bin/
#   3. Adds ~/.caravan/bin and ~/.dotnet to PATH (shell rc)
#
# After install: caravan-csharp build, run, test — everything just works.

set -e

CARAVAN_HOME="$HOME/.caravan"
CARAVAN_BIN="$CARAVAN_HOME/bin"
DOTNET_DIR="$HOME/.dotnet"

echo "=== caravan-csharp installer ==="
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

# --- Step 2: Install caravan-csharp build tool ---

echo "[2/3] Installing caravan-csharp build tool..."
mkdir -p "$CARAVAN_HOME" "$CARAVAN_BIN"

# Use local copy if running from clone, otherwise download from GitHub
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd)"
if [[ -f "$SCRIPT_DIR/build-tool/caravan-csharp" ]]; then
    cp "$SCRIPT_DIR/build-tool/caravan-csharp" "$CARAVAN_BIN/caravan-csharp"
    echo "       copied from local clone"
else
    curl -sSL "https://raw.githubusercontent.com/CaravanScale/caravan/master/caravan-csharp/build-tool/caravan-csharp" \
        -o "$CARAVAN_BIN/caravan-csharp"
    echo "       downloaded from GitHub"
fi
chmod +x "$CARAVAN_BIN/caravan-csharp"

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

setup_path "$CARAVAN_BIN" "caravan tools"
setup_path "$DOTNET_DIR" "dotnet SDK"
echo "[3/3] PATH configured"

# --- Verify ---

echo ""
echo "Verifying installation..."
if command -v caravan-csharp &>/dev/null; then
    echo "  caravan-csharp: $(which caravan-csharp)"
else
    echo "  caravan-csharp: $CARAVAN_BIN/caravan-csharp"
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
echo "Done! In a project with caravan.toml:"
echo "  caravan-csharp build    # Native AOT binary"
echo "  caravan-csharp run      # Build + run"
echo "  caravan-csharp test     # Run tests"
echo ""
echo "Note: restart your shell or run:"
echo "  export PATH=\"\$HOME/.caravan/bin:\$HOME/.dotnet:\$PATH\""
