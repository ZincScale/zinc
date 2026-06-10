#!/usr/bin/env bash
# End-to-end plugin test: rebar3 compile (zinc pre-hook transpiles) -> run on BEAM.
# Erlang+rebar3 run in docker; the host JDK is mounted in for the transpiler.
set -euo pipefail
cd "$(dirname "$0")"

REBAR3="${REBAR3:-$HOME/.cache/zinc/rebar3}"
if [ ! -x "$REBAR3" ]; then
  mkdir -p "$(dirname "$REBAR3")"
  curl -sSfL -o "$REBAR3" https://github.com/erlang/rebar3/releases/latest/download/rebar3
  chmod +x "$REBAR3"
fi

JAVA_DIR="${JAVA_DIR:-$HOME/.local/java/current}"
ROOT="$(cd .. && pwd)"   # beam-transpiler (= ZINC_HOME)

rm -rf test/fixture/hello/_build test/fixture/hello/src/zinc_gen

got=$(docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
  -v "$ROOT":/work -v "$JAVA_DIR":/java -v "$REBAR3":/usr/local/bin/rebar3 \
  -e PATH="/java/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin" \
  -e ZINC_HOME=/work \
  -w /work/rebar_zinc/test/fixture/hello erlang:slim \
  sh -c 'rebar3 compile 1>&2 && erl -noshell -pa _build/default/lib/hello/ebin -eval "main:main(), init:stop()."')

if [ "$got" = "hello-rebar" ]; then
  echo "PASS  rebar_zinc  ->  $got"
else
  echo "FAIL  rebar_zinc  ->  got '$got'  want 'hello-rebar'"
  exit 1
fi
