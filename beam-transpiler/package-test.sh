#!/usr/bin/env bash
# Release artifact smoke: build the zc tarball, assert it contains only runtime assets,
# and run the packaged CLI.
set -euo pipefail
cd "$(dirname "$0")"

VER="${1:-0.0.0-smoke}"
TARBALL="dist/zc-$VER.tar.gz"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

./package.sh "$VER" >/dev/null

contents="$(tar -tzf "$TARBALL")"
if printf '%s\n' "$contents" | grep -Eq '(^|/)(test|_build|zinc_gen)(/|$)|\.zinc$'; then
  printf 'FAIL  package contents include retired/generated assets\n' >&2
  printf '%s\n' "$contents" >&2
  exit 1
fi

tar -xzf "$TARBALL" -C "$TMP"
got="$("$TMP/zc-$VER/bin/zc" version)"
if [ "$got" != "zc 0.1.0 (zinc on BEAM)" ]; then
  printf "FAIL  package zc version -> got '%s'\n" "$got" >&2
  exit 1
fi

echo "PASS  package  ->  $TARBALL"
