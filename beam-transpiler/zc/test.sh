#!/usr/bin/env bash
# zc end-to-end: init a project, zc run it on BEAM (rebar3 + erl in docker, host JDK mounted).
set -euo pipefail
cd "$(dirname "$0")"

REBAR3="${REBAR3:-$HOME/.cache/zinc/rebar3}"
if [ ! -x "$REBAR3" ]; then
  mkdir -p "$(dirname "$REBAR3")"
  curl -sSfL -o "$REBAR3" https://github.com/erlang/rebar3/releases/latest/download/rebar3
  chmod +x "$REBAR3"
fi
JAVA_DIR="${JAVA_DIR:-$HOME/.local/java/current}"
ROOT="$(cd .. && pwd)"

got=$(docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
  -v "$ROOT":/work -v "$JAVA_DIR":/java -v "$REBAR3":/usr/local/bin/rebar3 \
  -e PATH="/java/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin" \
  -w /tmp erlang:slim \
  sh -c '/work/bin/zc init demo 1>&2 && cd demo && /work/bin/zc run' | tail -1)

if [ "$got" = "Hello from demo!" ]; then
  echo "PASS  zc  ->  $got"
else
  echo "FAIL  zc  ->  got '$got'  want 'Hello from demo!'"
  exit 1
fi
