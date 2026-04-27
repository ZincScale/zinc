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

# E2E test runner for zinc-python (Zinc → Python transpiler).
# Invokes the installed `zinc-python` CLI (~/.zinc-python/bin/zinc-python)
# which routes through uv to Python 3.14t — no need for the test runner
# to know about uv or the Python version.
DIR="$(cd "$(dirname "$0")" && pwd)"
ZINC="${ZINC_PYTHON:-$HOME/.zinc-python/bin/zinc-python}"
E2E_DIR="$DIR/e2e"
PASS=0
FAIL=0

if [ ! -x "$ZINC" ]; then
    echo "error: zinc-python CLI not found at $ZINC"
    echo "  install via: bash zinc-python/install.sh"
    echo "  or set ZINC_PYTHON=/path/to/zinc-python before running"
    exit 1
fi

for zn in "$E2E_DIR"/*.zn; do
    name=$(basename "$zn" .zn)
    expected="$E2E_DIR/expected/${name}.txt"

    if [ ! -f "$expected" ]; then
        echo "SKIP: $name (no expected output)"
        continue
    fi

    actual=$("$ZINC" run "$zn" 2>&1)
    exp=$(cat "$expected")

    if [ "$actual" = "$exp" ]; then
        echo "PASS: $name"
        PASS=$((PASS + 1))
    else
        echo "FAIL: $name"
        echo "  expected: $(echo "$exp" | head -3)"
        echo "  got:      $(echo "$actual" | head -3)"
        FAIL=$((FAIL + 1))
    fi
done

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ $FAIL -eq 0 ] || exit 1
