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
