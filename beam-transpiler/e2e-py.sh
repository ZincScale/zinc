#!/usr/bin/env bash
# End-to-end for the braces-Python surface (.zn). EVERYTHING runs through `zc` — the tooling
# we ship — on the managed OTP: build the zc jar once, then `zc run --once` each case (run the
# entry and exit, even for an Application). No direct java/erlc/erl. The only docker use is the
# Postgres sidecar the SQL case needs as external infra; its build+run still go through `zc`.
set -uo pipefail
cd "$(dirname "$0")"

JBIN="$(dirname "${JAVA_BIN:-$HOME/.local/java/current/bin/java}")"
[ -x "$JBIN/javac" ] || JBIN="$(dirname "$(command -v javac)")"
D="$PWD/dist/e2e"; rm -rf "$D"; mkdir -p "$D/classes"   # absolute: survives `cd` in subshells
if ! "$JBIN/javac" -d "$D/classes" $(find src/zinc -name '*.java') zc/Zc.java; then
  echo "FAIL  zc build"; exit 1
fi
printf 'Main-Class: Zc\n' > "$D/manifest.txt"
"$JBIN/jar" cfm "$D/zc.jar" "$D/manifest.txt" -C "$D/classes" .
cp -r rebar_zinc "$D/rebar_zinc"   # rebar transpile plugin lives next to the jar (ZINC_HOME_LIB)
# every case goes through this; timeout is a safety net so no run can hang the suite
zc() { timeout 180 "$JBIN/java" -DZINC_HOME_LIB="$D" -jar "$D/zc.jar" "$@"; }

examples=(hello countdown functions fizzbuzz counter counter_init supervised ffi channel protocols fstring trycatch exceptions records match multifile collections dict bools floats ternary strings breakcont math selfheal nested recursion json fileio http_client http_facade filestream pipeline veneer sealed encoding record_model webauth resources)
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
  [sealed]=$'ok d\nroute big\nfail negative'
  [encoding]=$'aGk=\ntrue\n4142\ntrue\ntrue'
  [record_model]=$'user\n8\nvin\n2'
  [webauth]=$'true\n64\n64\n36\n0\ntrue'
  [resources]=$'a.txt\ntxt\n3\ntrue\ntrue\ntrue'
)

fail=0

# positives: `zc run --once` each (a file, or a folder of .zn for multi-file), assert stdout
for ex in "${examples[@]}"; do
  src="examples/py/$ex.zn"
  [ -d "examples/py/$ex" ] && src="examples/py/$ex"
  got=$(zc run --once "$src" 2>"$D/$ex.err")
  if [ "$got" = "${want[$ex]}" ]; then
    echo "PASS  $ex  ->  $(printf '%s' "$got" | head -1)"
  elif grep -q "Unable to load crypto" "$D/$ex.err"; then
    echo "SKIP  $ex  (managed OTP crypto NIF unavailable in this env — OpenSSL mismatch)"
  else
    echo "FAIL  $ex  ->  got '$got'  want '${want[$ex]}'"; sed 's/^/    /' "$D/$ex.err"; fail=1
  fi
done

# SQL: a real zc PROJECT built + run through `zc` against a Postgres sidecar — the full
# rebar path (epgsql vendored in _checkouts, HEX_OFFLINE so no hex/firewall), all through the
# tooling. The project's _build is kept across runs so epgsql compiles once, not every time.
EPG="$PWD/dogfood/sqldemo/_checkouts/epgsql"; SQL_PG=zincsql-pg
sql_cleanup() { docker rm -f "$SQL_PG" >/dev/null 2>&1; }
if [ ! -d "$EPG/src" ] || ! command -v docker >/dev/null 2>&1; then
  echo "SKIP  sql (no epgsql checkout or docker)"
else
  sql_cleanup; pgok=
  for img in public.ecr.aws/docker/library/postgres:16-alpine postgres:16-alpine; do
    docker run -d --name "$SQL_PG" -p 5432:5432 -e POSTGRES_USER=zinc \
      -e POSTGRES_PASSWORD=zinc -e POSTGRES_DB=zinc -e POSTGRES_HOST_AUTH_METHOD=trust \
      "$img" >/dev/null 2>&1 && { pgok=1; break; }
    docker rm -f "$SQL_PG" >/dev/null 2>&1
  done
  if [ -z "$pgok" ]; then
    echo "SKIP  sql (no postgres image available)"
  else
    for i in $(seq 1 40); do docker exec "$SQL_PG" pg_isready -U zinc >/dev/null 2>&1 && break; sleep 1; done
    sleep 1
    proj="$PWD/dist/sqlproj"; mkdir -p "$proj/src" "$proj/_checkouts"  # persistent: keep _build
    [ -e "$proj/_checkouts/epgsql" ] || ln -s "$EPG" "$proj/_checkouts/epgsql"
    printf '[project]\nname = "sqldemo"\nversion = "0.1.0"\n\n[otp]\nversion = "29"\n\n[deps]\nepgsql = "4.7.1"\n' > "$proj/zinc.toml"
    sed 's#zincsql-pg:5432#localhost:5432#' examples/py/sql.zn > "$proj/src/main.zn"
    ( cd "$proj" && HEX_OFFLINE=1 zc run --once . ) >"$D/sql.out" 2>"$D/sql.err"
    got=$(cat "$D/sql.out")
    sqlwant=$'1\nvin\n7\n1\n2\nrolled back 2\nsql error caught'
    if [ "$got" = "$sqlwant" ]; then
      echo "PASS  sql  ->  $(printf '%s' "$got" | tr '\n' '|')"
    else
      echo "FAIL  sql  ->  got '$got'"; sed 's/^/    /' "$D/sql.err"; fail=1
    fi
  fi
  sql_cleanup
fi

# negatives: `zc run` MUST fail with the expected message fragment (the source-map contract)
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
  [nonexhaustive]="non-exhaustive match on R: missing C"
)
for ex in "${!wanterr[@]}"; do
  if zc run --once "examples/py_neg/$ex.zn" >/dev/null 2>"$D/neg_$ex.err"; then
    echo "FAIL  neg/$ex  ->  ran, expected error '${wanterr[$ex]}'"; fail=1; continue
  fi
  if grep -qF "${wanterr[$ex]}" "$D/neg_$ex.err"; then
    echo "PASS  neg/$ex"
  else
    echo "FAIL  neg/$ex  ->  got '$(head -1 "$D/neg_$ex.err")'  want '${wanterr[$ex]}'"; fail=1
  fi
done
exit $fail
