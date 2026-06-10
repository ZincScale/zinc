#!/usr/bin/env bash
# End-to-end: source -> Dart transpiler -> Erlang modules -> erlc -> run on BEAM -> assert output.
set -uo pipefail
cd "$(dirname "$0")"
mkdir -p out
# --user keeps files written into the mount owned by the host user.
DART="docker run --rm --user $(id -u):$(id -g) -e PUB_CACHE=/tmp/pub -v $PWD:/app -w /app dart:stable"
ERL="docker run --rm --user $(id -u):$(id -g) -v $PWD:/app -w /app erlang:slim"

examples=(sum_evens first_over countdown structs arrays strings bools elseif floats breakcont)
declare -A want=(
  [sum_evens]=20
  [first_over]=7
  [countdown]=15
  [structs]=185
  [arrays]=100
  [strings]=hi-BEAM-3
  [bools]=yes
  [elseif]=2
  [floats]=5.0
  [breakcont]=8
)
fail=0
for ex in "${examples[@]}"; do
  dir="out/$ex"
  rm -rf "$dir" && mkdir -p "$dir"
  if ! $DART dart bin/main.dart "examples/$ex.src" "$dir" >/dev/null 2>"$dir/transpile.err"; then
    echo "FAIL  $ex (transpile)"; sed 's/^/    /' "$dir/transpile.err"; fail=1; continue
  fi
  if ! $ERL erlc -o "$dir" "$dir"/*.erl 2>"$dir/compile.err"; then
    echo "FAIL  $ex (erlc)"; sed 's/^/    /' "$dir/compile.err"; fail=1; continue
  fi
  got=$($ERL erl -noshell -pa "$dir" -eval "main:main(), init:stop()." 2>"$dir/run.err")
  if [ "$got" = "${want[$ex]}" ]; then
    echo "PASS  $ex  ->  $got"
  else
    echo "FAIL  $ex  ->  got '$got'  want '${want[$ex]}'"
    for f in "$dir"/*.erl; do
      echo "----- generated $(basename "$f") -----"; sed 's/^/    /' "$f"
    done
    echo "----- run stderr -----"; sed 's/^/    /' "$dir/run.err"
    fail=1
  fi
done
exit $fail
