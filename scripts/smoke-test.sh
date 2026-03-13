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

# Smoke test: transpile + compile + run every example .zn file
# Usage: scripts/smoke-test.sh [path-to-zinc-binary]
set -euo pipefail

ZINC="$(cd "$(dirname "${1:-./zinc}")" && pwd)/$(basename "${1:-./zinc}")"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

if [ ! -x "$ZINC" ]; then
  echo "Building zinc..."
  (cd "$REPO_ROOT" && go build -o "$ZINC" ./cmd/zinc/)
fi

PASS=0
FAIL=0
FAILED_FILES=""

# Single-file examples: copy to temp dir so generated .go files don't pollute the repo
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

for f in "$REPO_ROOT"/examples/*.zn; do
  name=$(basename "$f" .zn)
  echo -n "  $name ... "
  cp "$f" "$TMPDIR/"
  if output=$("$ZINC" "$TMPDIR/$(basename "$f")" --run 2>&1); then
    echo "OK"
    PASS=$((PASS + 1))
  else
    echo "FAIL"
    echo "$output" | head -10
    FAIL=$((FAIL + 1))
    FAILED_FILES="$FAILED_FILES $name"
  fi
done

# Multi-file project: copy to temp dir to avoid overwriting committed .go files
MYAPP_TMP=$(mktemp -d)
trap 'rm -rf "$TMPDIR" "$MYAPP_TMP"' EXIT
cp -r "$REPO_ROOT/examples/myapp" "$MYAPP_TMP/myapp"
echo -n "  myapp (multi-file) ... "
if output=$("$ZINC" run "$MYAPP_TMP/myapp" 2>&1); then
  echo "OK"
  PASS=$((PASS + 1))
else
  echo "FAIL"
  echo "$output" | head -10
  FAIL=$((FAIL + 1))
  FAILED_FILES="$FAILED_FILES myapp"
fi

echo ""
echo "Results: $PASS passed, $FAIL failed"

if [ $FAIL -gt 0 ]; then
  echo "Failed:$FAILED_FILES"
  exit 1
fi
