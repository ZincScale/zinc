#!/usr/bin/env bash
# Copyright 2026 victorybhg
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

# Checks that all Java source files have the Apache 2.0 copyright header.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MISSING=()

while IFS= read -r -d '' file; do
  if ! head -n 3 "$file" | grep -q "Copyright 2026 victorybhg"; then
    MISSING+=("$file")
  fi
done < <(find "$REPO_ROOT/compiler" -name '*.java' -print0)

if [ ${#MISSING[@]} -eq 0 ]; then
  echo "All Java source files have license headers."
  exit 0
else
  echo "ERROR: The following files are missing the Apache 2.0 license header:"
  for f in "${MISSING[@]}"; do
    echo "  $f"
  done
  exit 1
fi
