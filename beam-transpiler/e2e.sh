#!/usr/bin/env bash
# End-to-end: source -> Java transpiler -> Erlang modules -> erlc -> run on BEAM -> assert output.
set -uo pipefail
cd "$(dirname "$0")"
mkdir -p out
# Multi-file source launcher (JDK 22+): java compiles src/ in memory, no build tool.
JAVA="${JAVA_BIN:-$HOME/.local/java/current/bin/java}"
command -v "$JAVA" >/dev/null || JAVA=java
# --user keeps files written into the mount owned by the host user.
ERL="docker run --rm --user $(id -u):$(id -g) -v $PWD:/app -w /app erlang:slim"

examples=(sum_evens first_over countdown structs arrays strings bools elseif floats breakcont multifile actor_counter actor_selfheal actor_children exceptions interfaces guards logging http_client json ffi atoms_tuples lambdas hashmap trycatch javastrings javacollections actor_args switchenum tcpserver close modifiers)
declare -A want=(
  [sum_evens]=20
  [first_over]=7
  [countdown]=15
  [structs]=185
  [arrays]=117
  [strings]=$'hi-BEAM-3\nline1\nline2\nsay "hi"'
  [bools]=yes
  [elseif]=2
  [floats]=5.0
  [breakcont]=12
  [multifile]=$'[10]\n1'
  [actor_counter]=7
  [actor_children]=7
  [exceptions]=$'8\nno such id\n1\nlocal'
  [interfaces]=$'hello zinc\nbeam!\nlambda fun'
  [guards]=$'badtype caught\n42'
  [logging]='clean stdout'
  [http_client]=$'200\nzinc!\nconnect refused caught'
  [json]=$'vin\n41\nsf\n40\n7\nmissing caught'
  [actor_selfheal]=$'3\n0\n1'
  [ffi]="BEAM-9"
  [atoms_tuples]=$'3\n42\nok'
  [lambdas]=22
  [hashmap]=12
  [trycatch]=$'7\ncaught\n2'
  [javastrings]=$'12\nHELLO, BEAM!\nBEAM\n2\nyes\n7'
  [javacollections]=$'2\n5\n41\ntrue'
  [actor_args]=43
  [switchenum]=$'cool\nmany'
  [tcpserver]=$'HELLO WORLD\nBEAM ME UP'
  [close]=$'1\nclosed db'
  [modifiers]=$'21\ns3cret'
)
# negative cases: each must FAIL transpile with the expected message fragment
declare -A wanterr=(
  [return_type]="return: cannot bind a String to int"
  [return_void]="void method cannot return a value"
  [arg_type]="f arg 1 ('x'): cannot bind a String to int"
  [actor_arg]="C.bump arg 1 ('by'): cannot bind a String to int"
  [field_init]="field n: cannot bind a String to int"
  [reassign]="x: cannot bind a String to int"
  [record_ctor]="new Point arg 1 ('x'): cannot bind a String to int"
  [void_value]="cannot use a void method's result"
  [spawn_arg]="new Counter arg 1 ('start'): cannot bind a String to int"
  [exc_field]="throw new Boom ('message'): cannot bind a int to String"
  [app_child_arg]="new Db arg 1 ('url'): cannot bind a int to String"
  [lambda_ret]="return: cannot bind a int to String"
  [mod_protected]="'protected' has no meaning"
  [mod_nonstatic]="Main.f: utility-class methods are static"
  [mod_static_actor]="C.get: 'static' does not belong here"
  [mod_private_actor]="C.get: cannot be private"
  [mod_private_cross]="Util.secret is private"
  [mod_final_local]="final variable 'x' cannot be reassigned"
  [mod_final_field]="final field 'n' cannot be reassigned"
  [mod_main_nonpublic]="public static void main"
)

fail=0
for ex in "${!wanterr[@]}"; do
  dir="out/neg_$ex"
  rm -rf "$dir" && mkdir -p "$dir"
  if "$JAVA" src/zinc/Main.java "examples/neg/$ex.zinc" "$dir" >/dev/null 2>"$dir/err"; then
    echo "FAIL  neg/$ex  ->  transpiled, expected error '${wanterr[$ex]}'"; fail=1; continue
  fi
  if grep -qF "${wanterr[$ex]}" "$dir/err"; then
    echo "PASS  neg/$ex  ->  $(cat "$dir/err")"
  else
    echo "FAIL  neg/$ex  ->  got '$(cat "$dir/err")'  want '${wanterr[$ex]}'"; fail=1
  fi
done

for ex in "${examples[@]}"; do
  dir="out/$ex"
  rm -rf "$dir" && mkdir -p "$dir"
  src="examples/$ex.zinc"
  [ -d "examples/$ex" ] && src="examples/$ex"   # project mode: a directory of .zinc files
  if ! "$JAVA" src/zinc/Main.java "$src" "$dir" >/dev/null 2>"$dir/transpile.err"; then
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
