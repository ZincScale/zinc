#!/usr/bin/env bash
# End-to-end for the braces-Python surface (.zn): source -> PyParser -> Ast -> CodeGen
# -> Erlang -> erlc -> run on BEAM -> assert stdout. Same pipeline as e2e.sh; only the
# frontend differs (Main dispatches .zn to PyLexer/PyParser).
set -uo pipefail
cd "$(dirname "$0")"
JAVA="${JAVA_BIN:-$HOME/.local/java/current/bin/java}"
command -v "$JAVA" >/dev/null || JAVA=java
ERL="docker run --rm --user $(id -u):$(id -g)"

examples=(hello countdown functions fizzbuzz counter counter_init supervised ffi channel protocols fstring trycatch exceptions records match multifile collections dict bools floats ternary strings breakcont math selfheal nested recursion json fileio http_client filestream)
declare -A want=(
  [hello]='Hello from braces-Python on BEAM!'
  [countdown]=15
  [functions]=$'7\n42'
  [fizzbuzz]=$'1\n2\nFizz\n4\nBuzz'
  [counter]=7
  [counter_init]=42
  [supervised]=$'7\n0\n5'
  [ffi]=BEAM
  [channel]=$'5\nx0x1x2x3x4'
  [protocols]=$'hello zinc\nbeam!\nlambda fun'
  [fstring]=$'hello zinc, n=7, sum=8\nCounter(count=41)'
  [trycatch]=$'7\n2'
  [exceptions]=$'8\nno such id\n1\nlocal'
  [records]=$'185\ngreen'
  [match]=$'red\ncool\none\nmany'
  [multifile]=$'[10]\n1'
  [collections]=$'40\n3\n21'
  [dict]=$'13\nlocalhost'
  [bools]=$'false\ntrue\ntrue\ntrue'
  [floats]=$'3.5\n8.0\n3\n2.5'
  [ternary]=$'big\n7'
  [strings]=$'12\nHELLO, BEAM!\nBEAM\ntrue'
  [breakcont]=6
  [math]=$'9\n4\n2'
  [selfheal]=$'5\n0'
  [nested]=$'1\n3'
  [recursion]=120
  [json]=$'vin\n41\nsf\n40\n7\nmissing caught'
  [fileio]=$'2\ntrue\n20\n1\ntrue\nno such file or directory: /tmp/zinc_fileio_py/missing.conf\nfalse'
  [http_client]='connect refused caught'
  [filestream]=$'5\ntrue'
)

fail=0
for ex in "${examples[@]}"; do
  dir="out/py_$ex"
  rm -rf "$dir" && mkdir -p "$dir"
  src="examples/py/$ex.zn"
  [ -d "examples/py/$ex" ] && src="examples/py/$ex"   # directory = multi-file project
  if ! "$JAVA" src/zinc/Main.java "$src" "$dir" >/dev/null 2>"$dir/transpile.err"; then
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

# negative cases: each MUST fail transpile with the expected message fragment (errors
# carry <file>:<line> where they originate -- the source-map contract).
declare -A wanterr=(
  [type_local]='type_local.zn:2: x: cannot bind a String to int'
  [return_void]='return_void.zn:2: return: void method cannot return a value'
  [return_type]='return_type.zn:2: return: cannot bind a String to int'
  [reassign]='reassign.zn:3: x: cannot bind a String to int'
  [arg_type]="arg_type.zn:3: add arg 2 ('b'): cannot bind a String to int"
  [parse_brace]="parse_brace.zn: Parse error: expected '}' but got EOF"
  [app_method]='Application Main can only declare main()'
  [two_apps]='more than one Application'
  [infer_cycle]="cannot infer return type — annotate it with '-> T'"
  [untyped_param]="parameter 'a' needs a type"
)
for ex in "${!wanterr[@]}"; do
  dir="out/pyneg_$ex"
  rm -rf "$dir" && mkdir -p "$dir"
  if "$JAVA" src/zinc/Main.java "examples/py_neg/$ex.zn" "$dir" >/dev/null 2>"$dir/err"; then
    echo "FAIL  neg/$ex  ->  transpiled, expected error '${wanterr[$ex]}'"; fail=1; continue
  fi
  if grep -qF "${wanterr[$ex]}" "$dir/err"; then
    echo "PASS  neg/$ex  ->  $(head -1 "$dir/err")"
  else
    echo "FAIL  neg/$ex  ->  got '$(head -1 "$dir/err")'  want '${wanterr[$ex]}'"; fail=1
  fi
done
exit $fail
