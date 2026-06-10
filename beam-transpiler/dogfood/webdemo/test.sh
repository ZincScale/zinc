#!/usr/bin/env bash
# Cowboy dogfood: zc run inside docker (fetches cowboy from hex on first run).
set -euo pipefail
cd "$(dirname "$0")"

REBAR3="${REBAR3:-$HOME/.cache/zinc/rebar3}"
JAVA_DIR="${JAVA_DIR:-$HOME/.local/java/current}"
ROOT="$(cd ../.. && pwd)"

got=$(docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
  -v "$ROOT":/work -v "$JAVA_DIR":/java -v "$REBAR3":/usr/local/bin/rebar3 \
  -e PATH="/java/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin" \
  -w /work/dogfood/webdemo erlang:slim \
  sh -c 'timeout 120 /work/bin/zc run' | tail -1)

if [ "$got" = "hello from zinc" ]; then
  echo "PASS  webdemo  ->  $got"
else
  echo "FAIL  webdemo  ->  got '$got'  want 'hello from zinc'"
  exit 1
fi
