#!/usr/bin/env bash
# End-to-end: source -> Dart transpiler -> Erlang -> run on BEAM -> assert output.
set -uo pipefail
cd "$(dirname "$0")"
mkdir -p out
DART="docker run --rm -v $PWD:/app -w /app dart:stable"
ERL="docker run --rm -v $PWD:/app -w /app erlang:slim"

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
  if ! $DART dart bin/main.dart "examples/$ex.src" > "out/$ex.erl" 2>"out/$ex.dart.err"; then
    echo "FAIL  $ex (transpile)"; sed 's/^/    /' "out/$ex.dart.err"; fail=1; continue
  fi
  got=$($ERL escript "out/$ex.erl" 2>"out/$ex.run.err")
  if [ "$got" = "${want[$ex]}" ]; then
    echo "PASS  $ex  ->  $got"
  else
    echo "FAIL  $ex  ->  got '$got'  want '${want[$ex]}'"
    echo "----- generated $ex.erl -----"; sed 's/^/    /' "out/$ex.erl"
    echo "----- run stderr -----"; sed 's/^/    /' "out/$ex.run.err"
    fail=1
  fi
done
exit $fail
