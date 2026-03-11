#!/usr/bin/env bash
# Checks that all Go source files (except examples/ and vendor/) have the
# Apache 2.0 copyright header.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MISSING=()

while IFS= read -r -d '' file; do
  # Check first 3 lines for copyright notice
  if ! head -n 3 "$file" | grep -q "Copyright 2026 victorybhg"; then
    MISSING+=("$file")
  fi
done < <(find "$REPO_ROOT" -name '*.go' \
  -not -path '*/examples/*' \
  -not -path '*/vendor/*' \
  -print0)

if [ ${#MISSING[@]} -eq 0 ]; then
  echo "All Go source files have license headers."
  exit 0
else
  echo "ERROR: The following files are missing the Apache 2.0 license header:"
  for f in "${MISSING[@]}"; do
    echo "  $f"
  done
  exit 1
fi
