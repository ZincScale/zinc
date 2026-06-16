#!/usr/bin/env bash
# Cowboy dogfood: zc run inside docker (zc vendors cowboy+deps into _checkouts first).
# The Application serves forever (static children), so timeout reaping it is the
# expected exit; the assertion is on main()'s client output.
set -euo pipefail
cd "$(dirname "$0")"

REBAR3="${REBAR3:-$HOME/.cache/zinc/rebar3}"
JAVA_DIR="${JAVA_DIR:-$HOME/.local/java/current}"
ROOT="$(cd ../.. && pwd)"

got=$(docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
  -v "$ROOT":/work -v "$JAVA_DIR":/java -v "$REBAR3":/usr/local/bin/rebar3 \
  -e PATH="/java/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin" \
  -w /work/dogfood/webdemo erlang:slim \
  sh -c 'timeout 120 /work/bin/zc run; true' | tail -5)

want=$(printf '201\nvin\nhello from zinc\n100\ntrue')

if [ "$got" = "$want" ]; then
  echo "PASS  webdemo"
else
  echo "FAIL  webdemo"
  echo "--- want ---"; echo "$want"
  echo "--- got ----"; echo "$got"
  exit 1
fi
