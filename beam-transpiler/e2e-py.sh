#!/usr/bin/env bash
# End-to-end for the braces-Python surface (.zn): source -> PyParser -> Ast -> CodeGen
# -> Erlang -> erlc -> run on BEAM -> assert stdout. Same pipeline as e2e.sh; only the
# frontend differs (Main dispatches .zn to PyLexer/PyParser).
set -uo pipefail
cd "$(dirname "$0")"
mkdir -p out
JAVA="${JAVA_BIN:-$HOME/.local/java/current/bin/java}"
command -v "$JAVA" >/dev/null || JAVA=java
ERL="docker run --rm --user $(id -u):$(id -g) -v $PWD:/app -w /app erlang:slim"
# compile the transpiler ONCE, then run it as a precompiled class per example
# (java source-launch recompiled all of src/ in-memory on every invocation -- ~4x slower).
JAVAC="${JAVA%java}javac"; command -v "$JAVAC" >/dev/null || JAVAC=javac
CLASSES="out/.classes_py"; rm -rf "$CLASSES" && mkdir -p "$CLASSES"  # own dir: safe alongside e2e.sh
if ! "$JAVAC" -d "$CLASSES" src/zinc/*.java; then echo "FAIL  transpiler compile"; exit 1; fi
ZC=("$JAVA" -cp "$CLASSES" zinc.Main)

examples=(hello countdown functions fizzbuzz counter counter_init supervised ffi channel protocols fstring trycatch exceptions records match multifile collections dict bools floats ternary strings breakcont math selfheal nested recursion json fileio http_client http_facade filestream pipeline veneer)
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
  [http_facade]='facade refused caught'
  [filestream]=$'5\ntrue'
  [pipeline]=$'5\ntrue'
  [veneer]=$'13\n3\n5\nlocalhost\n42!'
)

fail=0

# phase 1: transpile everything on the host (no docker)
runnable=()
for ex in "${examples[@]}"; do
  dir="out/py_$ex"
  rm -rf "$dir" && mkdir -p "$dir"
  src="examples/py/$ex.zn"
  [ -d "examples/py/$ex" ] && src="examples/py/$ex"   # directory = multi-file project
  if ! "${ZC[@]}" "$src" "$dir" >/dev/null 2>"$dir/transpile.err"; then
    echo "FAIL  $ex (transpile)"; sed 's/^/    /' "$dir/transpile.err"; fail=1; continue
  fi
  runnable+=("$ex")
done

# phase 2: ONE container compiles + runs every case (was one container spin per case)
rm -f out/.codes_py
{
  echo 'set -u'
  for ex in "${runnable[@]}"; do
    cat <<EOS
if erlc -o out/py_$ex out/py_$ex/*.erl 2> out/py_$ex/compile.err; then
  timeout 120 erl -noshell -pa out/py_$ex -eval "main:main(), init:stop()." \
    > out/py_$ex/run.out 2> out/py_$ex/run.err
  echo "$ex:\$?" >> out/.codes_py
else
  echo "$ex:erlc" >> out/.codes_py
fi
EOS
  done
} > out/.runner_py.sh
$ERL sh out/.runner_py.sh

# phase 3: assert each case from the artifacts
for ex in "${runnable[@]}"; do
  dir="out/py_$ex"
  code=$(grep "^$ex:" out/.codes_py | head -1 | cut -d: -f2)
  if [ "$code" = "erlc" ]; then
    echo "FAIL  $ex (erlc)"; sed 's/^/    /' "$dir/compile.err"; fail=1; continue
  fi
  got=$(cat "$dir/run.out")
  if [ "${code:-1}" -ne 0 ]; then
    echo "FAIL  $ex  ->  exit $code (want 0)"; sed 's/^/    /' "$dir/run.err"; fail=1
  elif [ "$got" = "${want[$ex]}" ]; then
    echo "PASS  $ex  ->  $got"
  else
    echo "FAIL  $ex  ->  got '$got'  want '${want[$ex]}'"; sed 's/^/    /' "$dir/run.err"; fail=1
  fi
done

# SQL e2e: real Postgres + epgsql, in its OWN networked container (the batch above has no
# DB). The Db pool connects in its constructors at boot, so pg must be up first. Skips
# cleanly if no postgres image is available (Docker Hub is rate-limited; we prefer the
# unauthenticated public ECR mirror). epgsql lives under src/{,commands,datatypes}.
SQL_NET=zinc_sql_net; SQL_PG=zincsql-pg; EPG="$PWD/dogfood/sqldemo/_checkouts/epgsql"
sql_cleanup() { docker rm -f "$SQL_PG" >/dev/null 2>&1; docker network rm "$SQL_NET" >/dev/null 2>&1; }
if [ ! -d "$EPG/src" ]; then
  echo "SKIP  sql (no epgsql checkout)"
else
  sql_cleanup; docker network create "$SQL_NET" >/dev/null 2>&1
  pgok=
  for img in public.ecr.aws/docker/library/postgres:16-alpine postgres:16-alpine; do
    if docker run -d --name "$SQL_PG" --network "$SQL_NET" \
         -e POSTGRES_USER=zinc -e POSTGRES_PASSWORD=zinc -e POSTGRES_DB=zinc \
         "$img" >/dev/null 2>&1; then pgok=1; break; fi
    docker rm -f "$SQL_PG" >/dev/null 2>&1
  done
  if [ -z "$pgok" ]; then
    echo "SKIP  sql (no postgres image available)"; sql_cleanup
  else
    for i in $(seq 1 40); do
      docker exec "$SQL_PG" pg_isready -U zinc >/dev/null 2>&1 && break; sleep 1
    done
    sleep 1
    dir=out/py_sql; rm -rf "$dir" && mkdir -p "$dir"
    if ! "${ZC[@]}" examples/py/sql.zn "$dir" >/dev/null 2>"$dir/transpile.err"; then
      echo "FAIL  sql (transpile)"; sed 's/^/    /' "$dir/transpile.err"; fail=1
    else
      got=$(docker run --rm --user "$(id -u):$(id -g)" --network "$SQL_NET" \
        -v "$PWD/$dir:/app" -v "$EPG:/epg" -w /app erlang:slim sh -c \
        'erlc -I /epg/include -o /tmp $(find /epg/src -name "*.erl") >/dev/null 2>&1
         erlc -pa /tmp -o . *.erl 2>cc.err && \
           erl -noshell -pa . -pa /tmp -eval "main:main(), init:stop()." 2>run.err')
      sqlwant=$'1\nvin\n7\n1\n2\nrolled back 2\nsql error caught'
      if [ "$got" = "$sqlwant" ]; then
        echo "PASS  sql  ->  $(printf '%s' "$got" | tr '\n' '|')"
      else
        echo "FAIL  sql  ->  got '$got'"; sed 's/^/    /' "$dir/run.err"; fail=1
      fi
    fi
    sql_cleanup
  fi
fi

# negative cases: each MUST fail transpile with the expected message fragment (errors
# carry <file>:<line> where they originate -- the source-map contract). Host-only, fast.
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
  if "${ZC[@]}" "examples/py_neg/$ex.zn" "$dir" >/dev/null 2>"$dir/err"; then
    echo "FAIL  neg/$ex  ->  transpiled, expected error '${wanterr[$ex]}'"; fail=1; continue
  fi
  if grep -qF "${wanterr[$ex]}" "$dir/err"; then
    echo "PASS  neg/$ex  ->  $(head -1 "$dir/err")"
  else
    echo "FAIL  neg/$ex  ->  got '$(head -1 "$dir/err")'  want '${wanterr[$ex]}'"; fail=1
  fi
done
exit $fail
