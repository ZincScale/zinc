#!/bin/sh
# Copyright 2026 victorybhg
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

# Zinc installer — installs Zinc compiler + toolchain (GraalVM JDK 25, Mill)
# Usage: curl -sSL https://raw.githubusercontent.com/ZincScale/zinc/master/install.sh | sh

set -e

REPO="ZincScale/zinc"
INSTALL_DIR="${ZINC_INSTALL_DIR:-$HOME/.local/bin}"
MILL_LAUNCHER_URL="https://raw.githubusercontent.com/com-lihaoyi/mill/main/mill"

echo "Zinc installer"
echo "=============="

# Check Java 25
check_java() {
    if command -v java >/dev/null 2>&1; then
        ver=$(java -version 2>&1 | head -1 | sed 's/.*"\([0-9]*\).*/\1/')
        if [ "$ver" -ge 25 ] 2>/dev/null; then
            echo "✓ Java $ver found"
            return 0
        fi
    fi
    echo "✗ Java 25+ required. Install GraalVM JDK 25:"
    echo "  https://www.graalvm.org/downloads/"
    return 1
}

# Check/install Mill
check_mill() {
    if command -v mill >/dev/null 2>&1; then
        echo "✓ Mill found"
        return 0
    fi
    echo "Installing Mill..."
    curl -L "$MILL_LAUNCHER_URL" > /tmp/mill && chmod +x /tmp/mill
    mkdir -p "$INSTALL_DIR"
    mv /tmp/mill "$INSTALL_DIR/mill"
    echo "✓ Mill installed to $INSTALL_DIR/mill"
}

# Install Zinc compiler
install_zinc() {
    echo "Installing Zinc compiler..."

    # Clone and build
    TMPDIR=$(mktemp -d)
    git clone --depth 1 "https://github.com/$REPO.git" "$TMPDIR/zinc" 2>/dev/null
    cd "$TMPDIR/zinc/compiler"
    make build 2>/dev/null

    # Install
    mkdir -p "$INSTALL_DIR"
    cp zinc-compiler.jar "$INSTALL_DIR/"
    cat > "$INSTALL_DIR/zinc" << 'LAUNCHER'
#!/bin/sh
exec java --enable-preview -jar "$(dirname "$0")/zinc-compiler.jar" "$@"
LAUNCHER
    chmod +x "$INSTALL_DIR/zinc"

    # Cleanup
    rm -rf "$TMPDIR"

    echo "✓ Zinc installed to $INSTALL_DIR/zinc"
}

# Main
check_java || exit 1
check_mill
install_zinc

echo ""
echo "Done! Make sure $INSTALL_DIR is in your PATH."
echo "  zinc init my-project"
echo "  zinc run my-project/src/main.zn"
