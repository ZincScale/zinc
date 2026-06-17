#!/usr/bin/env bash
# End-to-end for the braces-Python surface (.zn): source -> PyParser -> Ast -> CodeGen
# -> Erlang -> erlc -> run on BEAM -> assert stdout. Same pipeline as e2e.sh; only the
# frontend differs (Main dispatches .zn to PyLexer/PyParser).
set -uo pipefail
cd "$(dirname "$0")"
JAVA="${JAVA_BIN:-$HOME/.local/java/current/bin/java}"
command -v "$JAVA" >/dev/null || JAVA=java
ERL="docker run --rm --user $(id -u):$(id -g)"

examples=(hello countdown functions fizzbuzz counter counter_init)
declare -A want=(
  [hello]='Hello from braces-Python on BEAM!'
  [countdown]=15
  [functions]=$'7\n42'
  [fizzbuzz]=$'1\n2\nFizz\n4\nBuzz'
  [counter]=7
  [counter_init]=42
)

fail=0
for ex in "${examples[@]}"; do
  dir="out/py_$ex"
  rm -rf "$dir" && mkdir -p "$dir"
  if ! "$JAVA" src/zinc/Main.java "examples/py/$ex.zn" "$dir" >/dev/null 2>"$dir/transpile.err"; then
    echo "FAIL  $ex (transpile)"; sed 's/^/    /' "$dir/transpile.err"; fail=1; continue
  fi
  got=$($ERL -v "$PWD/$dir:/app" -w /app erlang:slim sh -c \
    'erlc -o . *.erl 2>cc.err && erl -noshell -pa . -eval "main:main(), init:stop()." 2>run.err' 2>/dev/null)
  if [ "$got" = "${want[$ex]}" ]; then
    echo "PASS  $ex  ->  $got"
  else
    echo "FAIL  $ex  ->  got '$got'  want '${want[$ex]}'"; fail=1
  fi
done
exit $fail
