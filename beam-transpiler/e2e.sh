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

examples=(sum_evens first_over countdown structs arrays strings bools elseif floats breakcont multifile actor_counter actor_selfheal actor_children exceptions interfaces guards logging http_client json ffi atoms_tuples lambdas hashmap trycatch javastrings javacollections actor_args switchenum tcpserver close modifiers arraylist warn_iget mathstr ternary fileio filestream filewrite proc_json proc_csv proc_binary)
declare -A want=(
  [sum_evens]=20
  [first_over]=7
  [countdown]=15
  [structs]=185
  [arrays]=117
  [strings]=$'hi-BEAM-3\nline1\nline2\nsay "hi"'
  [bools]=$'true\nfalse\ntrue'
  [elseif]=$'3\n2\n1'
  [floats]=$'5.0\n3\n3.5\n2'
  [breakcont]=12
  [multifile]=$'[10]\n1'
  [actor_counter]=7
  [actor_children]=$'7\n0\n5'
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
  [switchenum]=$'red\ncool\none\nmany'
  [tcpserver]=$'HELLO WORLD\nBEAM ME UP'
  [close]=$'1\nclosed db'
  [modifiers]=$'21\ns3cret'
  [arraylist]=$'500000\n250000\n7\n999997'
  [warn_iget]=$'8\n3'
  [mathstr]=$'12.0\n12\n32.0\n3.0\n4.0\na-b-c\n1\n-1\na\nc\ne\n-1\nx=7 (12.5%)\ntrue\nfalse'
  [ternary]=$'big\n0\n-1\n7'
  [fileio]=$'2\ntrue\n20\n1\ntrue\ntrue\nno such file or directory: /tmp/zinc_fileio/missing.conf\nfalse'
  [filestream]=$'500500\n1000'
  [filewrite]=$'5\ntrue\n6'
  [proc_json]=$'daily\n97\n100\n2\n30'
  [proc_csv]=$'rows=3 total=96'
  [proc_binary]=$'5\ntrue'
)
# transpile-warning contracts (substring against transpile stderr): warnings must
# fire where promised — and carry file:line
declare -A wantwarn=(
  [warn_iget]="warn_iget.zinc:6: List.get is O(n)"
)
# stderr contracts (substring match): streams are part of the language contract —
# Log.*/crash reports land on stderr, println on stdout
declare -A wantstderr=(
  [logging]="warning: about to be clean"
  [actor_selfheal]="child_terminated"
  [actor_children]="child_terminated"
)
# negative cases: each must FAIL transpile with the expected message fragment
declare -A wanterr=(
  [return_type]="return: cannot bind a String to int"
  [return_void]="void method cannot return a value"
  [arg_type]="f arg 1 ('x'): cannot bind a String to int"
  [actor_arg]="C.bump arg 1 ('by'): cannot bind a String to int"
  [field_init]="field n: cannot bind a String to int"
  [reassign]="reassign.zinc:2: x: cannot bind a String to int"
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
  [mod_final_local]="mod_final_local.zinc:4: final variable 'x' cannot be reassigned"
  [mod_final_field]="final field 'n' cannot be reassigned"
  [mod_main_nonpublic]="public static void main"
  [immutable_add]="List is read-only"
)

fail=0

# legal-Java gate (fast, host javac): the whole .zinc surface minus the erlang.* FFI
# basement must compile under javac against the prelude jar. Run first -- fail before
# the slow BEAM phase if any source stopped being valid Java.
if ./legaljava.sh; then echo "PASS  legal-Java gate"; else echo "FAIL  legal-Java gate"; fail=1; fi

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

# phase 1: transpile everything on the host (no docker involved)
runnable=()
for ex in "${examples[@]}"; do
  dir="out/$ex"
  rm -rf "$dir" && mkdir -p "$dir"
  src="examples/$ex.zinc"
  [ -d "examples/$ex" ] && src="examples/$ex"   # project mode: a directory of .zinc files
  if ! "$JAVA" src/zinc/Main.java "$src" "$dir" >/dev/null 2>"$dir/transpile.err"; then
    echo "FAIL  $ex (transpile)"; sed 's/^/    /' "$dir/transpile.err"; fail=1; continue
  fi
  runnable+=("$ex")
done

# phase 2: ONE container compiles + runs every case (was 2 container spins per case —
# ~110 per suite; this is the entire docker cost now)
rm -f out/.codes
{
  echo 'set -u'
  for ex in "${runnable[@]}"; do
    cat <<EOS
if erlc -o out/$ex out/$ex/*.erl 2> out/$ex/compile.err; then
  timeout 120 erl -noshell -pa out/$ex -eval "main:main(), init:stop()." \
    > out/$ex/run.out 2> out/$ex/run.err
  echo "$ex:\$?" >> out/.codes
else
  echo "$ex:erlc" >> out/.codes
fi
EOS
  done
} > out/.runner.sh
$ERL sh out/.runner.sh

# phase 3: assert each case from the artifacts
for ex in "${runnable[@]}"; do
  dir="out/$ex"
  code=$(grep "^$ex:" out/.codes | head -1 | cut -d: -f2)
  if [ "$code" = "erlc" ]; then
    echo "FAIL  $ex (erlc)"; sed 's/^/    /' "$dir/compile.err"; fail=1; continue
  fi
  got=$(cat "$dir/run.out")
  if [ "${code:-1}" -ne 0 ]; then
    # exit code is part of the contract: right output + dirty exit is a FAIL
    echo "FAIL  $ex  ->  exit $code (want 0)"
    echo "----- run stderr -----"; sed 's/^/    /' "$dir/run.err"
    fail=1
  elif [ "$got" != "${want[$ex]}" ]; then
    echo "FAIL  $ex  ->  got '$got'  want '${want[$ex]}'"
    for f in "$dir"/*.erl; do
      echo "----- generated $(basename "$f") -----"; sed 's/^/    /' "$f"
    done
    echo "----- run stderr -----"; sed 's/^/    /' "$dir/run.err"
    fail=1
  elif [ -n "${wantstderr[$ex]:-}" ] && ! grep -qF "${wantstderr[$ex]}" "$dir/run.err"; then
    echo "FAIL  $ex  ->  stderr missing '${wantstderr[$ex]}'"
    echo "----- run stderr -----"; sed 's/^/    /' "$dir/run.err"
    fail=1
  elif [ -n "${wantwarn[$ex]:-}" ] && ! grep -qF "${wantwarn[$ex]}" "$dir/transpile.err"; then
    echo "FAIL  $ex  ->  transpile warning missing '${wantwarn[$ex]}'"
    echo "----- transpile stderr -----"; sed 's/^/    /' "$dir/transpile.err"
    fail=1
  else
    echo "PASS  $ex  ->  $got"
  fi
done
exit $fail
