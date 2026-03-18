#!/usr/bin/env bash
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

# C# smoke test: transpile C#-only examples and run them
# Usage: scripts/smoke-test-csharp.sh [path-to-zinc-binary]
set -euo pipefail

ZINC="$(cd "$(dirname "${1:-./zinc}")" && pwd)/$(basename "${1:-./zinc}")"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

if [ ! -x "$ZINC" ]; then
  echo "Building zinc..."
  (cd "$REPO_ROOT" && go build -o "$ZINC" ./cmd/zinc/)
fi

# Verify dotnet is available
if ! command -v dotnet &>/dev/null; then
  # Try common locations
  for candidate in "$HOME/.dotnet/dotnet" /usr/local/bin/dotnet /usr/bin/dotnet; do
    if [ -x "$candidate" ]; then
      export PATH="$(dirname "$candidate"):$PATH"
      break
    fi
  done
fi

if ! command -v dotnet &>/dev/null; then
  echo "SKIP: dotnet SDK not found"
  exit 0
fi

echo "dotnet version: $(dotnet --version)"
echo ""

PASS=0
FAIL=0
FAILED_FILES=""

# --- Single-file C# examples ---
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Create a minimal zinc.toml for single-file transpile+run
cat > "$TMPDIR/zinc.toml" << 'EOF'
[project]
name = "smoketest"
version = "0.1.0"

[build]
target = "csharp"
optimize = false
EOF

# Examples that require .NET type resolver (NuGet imports, JsonSerializer, etc.)
SKIP_EXAMPLES="annotations"

for f in "$REPO_ROOT"/examples/csharp-only/*.zn; do
  name=$(basename "$f" .zn)

  # Skip examples that need type resolver
  if echo "$SKIP_EXAMPLES" | grep -qw "$name"; then
    echo "  $name ... SKIP (requires type resolver)"
    continue
  fi

  echo -n "  $name ... "

  # Copy the .zn file as main.zn so zinc run finds it
  cp "$f" "$TMPDIR/main.zn"

  if output=$("$ZINC" run "$TMPDIR" 2>&1); then
    echo "OK"
    PASS=$((PASS + 1))
  else
    echo "FAIL"
    echo "$output" | head -10
    FAIL=$((FAIL + 1))
    FAILED_FILES="$FAILED_FILES $name"
  fi

  # Clean build artifacts
  rm -rf "$TMPDIR/.zinc-build"
done

# --- zinc test smoke test ---
echo -n "  zinc-test ... "
TEST_DIR=$(mktemp -d)
cat > "$TEST_DIR/zinc.toml" << 'EOF'
[project]
name = "testsmoke"
version = "0.1.0"

[build]
target = "csharp"
optimize = false
EOF

cat > "$TEST_DIR/main.zn" << 'EOF'
Int add(Int a, Int b) {
    return a + b
}

main() {
    print(add(1, 2))
}
EOF

cat > "$TEST_DIR/smoke_test.zn" << 'EOF'
test_add() {
    assertEqual(add(1, 2), 3)
}

test_add_negative() {
    assertEqual(add(-1, 1), 0)
}
EOF

if output=$("$ZINC" test "$TEST_DIR" 2>&1); then
  echo "OK"
  PASS=$((PASS + 1))
else
  echo "FAIL"
  echo "$output" | head -10
  FAIL=$((FAIL + 1))
  FAILED_FILES="$FAILED_FILES zinc-test"
fi
rm -rf "$TEST_DIR"

echo ""
echo "Results: $PASS passed, $FAIL failed"

if [ $FAIL -gt 0 ]; then
  echo "Failed:$FAILED_FILES"
  exit 1
fi
