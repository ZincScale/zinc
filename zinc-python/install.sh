#!/bin/bash
# Copyright 2026 ZincScale
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

# Zinc installer — one command, zero dependencies
#
# Usage:
#   curl -LsSf https://raw.githubusercontent.com/ZincScale/zinc/master/install.sh | bash
#
# Installs:
#   - uv (Python package manager)
#   - Python 3.14t (free-threading)
#   - zinc compiler (transpiler + CLI)
#
# Everything goes into ~/.zinc/

set -e

ZINC_HOME="$HOME/.zinc-python"
ZINC_BIN="$ZINC_HOME/bin"
ZINC_REPO="https://github.com/CaravanScale/caravan.git"

echo "Installing zinc-python..."

# --- Step 1: Install uv if not present ---
if ! command -v uv &>/dev/null && [ ! -f "$ZINC_BIN/uv" ]; then
    echo "  installing uv..."
    curl -LsSf https://astral.sh/uv/install.sh | sh 2>/dev/null
fi

# Find uv
if command -v uv &>/dev/null; then
    UV="uv"
elif [ -f "$HOME/.local/bin/uv" ]; then
    UV="$HOME/.local/bin/uv"
elif [ -f "$HOME/.cargo/bin/uv" ]; then
    UV="$HOME/.cargo/bin/uv"
else
    echo "error: failed to install uv"
    exit 1
fi
echo "  uv: $($UV --version)"

# --- Step 2: Install Python 3.14t (free-threading) ---
echo "  installing Python 3.14t (free-threading)..."
$UV python install 3.14+freethreaded 2>&1 | grep -E "^(Installed|Python)" || true
echo "  python: $($UV run --python 3.14t python --version 2>/dev/null || echo 'installing...')"

# --- Step 3: Install zinc-python compiler ---
echo "  installing zinc-python compiler..."
mkdir -p "$ZINC_HOME" "$ZINC_BIN"

# Clone or update the repo. The compiler lives at zinc-python/compiler/
# inside the caravan monorepo.
if [ -d "$ZINC_HOME/src" ]; then
    cd "$ZINC_HOME/src" && git pull --quiet 2>/dev/null || true
else
    git clone --quiet --depth 1 "$ZINC_REPO" "$ZINC_HOME/src"
fi

# Create the zinc-python wrapper script. Named `zinc-python` so it
# coexists on PATH with zinc-go's `zinc` binary — the two transpilers
# target different output languages and users may have both installed.
cat > "$ZINC_BIN/zinc-python" << 'WRAPPER'
#!/bin/bash
# zinc-python CLI wrapper — uses uv to ensure Python 3.14t (free-threading)
ZINC_HOME="$HOME/.zinc-python"
COMPILER_DIR="$ZINC_HOME/src/zinc-python/compiler"

# Find uv
if command -v uv &>/dev/null; then UV="uv"
elif [ -f "$HOME/.local/bin/uv" ]; then UV="$HOME/.local/bin/uv"
elif [ -f "$HOME/.cargo/bin/uv" ]; then UV="$HOME/.cargo/bin/uv"
else echo "error: uv not found"; exit 1; fi

PYTHONPATH="$COMPILER_DIR" exec $UV run --quiet --python 3.14t python "$COMPILER_DIR/zinc.py" "$@"
WRAPPER
chmod +x "$ZINC_BIN/zinc-python"

# --- Step 4: Add to PATH ---
SHELL_RC=""
if [ -f "$HOME/.bashrc" ]; then SHELL_RC="$HOME/.bashrc"
elif [ -f "$HOME/.zshrc" ]; then SHELL_RC="$HOME/.zshrc"
elif [ -f "$HOME/.profile" ]; then SHELL_RC="$HOME/.profile"
fi

PATH_LINE='export PATH="$HOME/.zinc-python/bin:$PATH"'
if [ -n "$SHELL_RC" ] && ! grep -q '.zinc-python/bin' "$SHELL_RC" 2>/dev/null; then
    echo "" >> "$SHELL_RC"
    echo "# zinc-python" >> "$SHELL_RC"
    echo "$PATH_LINE" >> "$SHELL_RC"
    echo "  added ~/.zinc-python/bin to PATH in $SHELL_RC"
fi

echo ""
echo "zinc-python installed!"
echo ""
echo "  To start using zinc-python, run:"
echo "    export PATH=\"\$HOME/.zinc-python/bin:\$PATH\""
echo ""
echo "  Then:"
echo "    zinc-python init myapp"
echo "    cd myapp"
echo "    zinc-python run src/"
