#!/usr/bin/env bash
# End-to-end for the canonical Zinc-on-BEAM .zn surface. This intentionally stays separate
# from e2e-py.sh so the compatibility frontend can remain green while canonical syntax grows.
set -uo pipefail
cd "$(dirname "$0")"

JBIN="$(dirname "${JAVA_BIN:-$HOME/.local/java/current/bin/java}")"
[ -x "$JBIN/javac" ] || JBIN="$(dirname "$(command -v javac)")"
D="$PWD/dist/e2e-zinc"; rm -rf "$D"; mkdir -p "$D/classes"
if ! "$JBIN/javac" -d "$D/classes" $(find src/zinc -name '*.java') zc/Zc.java; then
  echo "FAIL  zc build"; exit 1
fi
printf 'Main-Class: Zc\n' > "$D/manifest.txt"
"$JBIN/jar" cfm "$D/zc.jar" "$D/manifest.txt" -C "$D/classes" .
cp -r rebar_zinc "$D/rebar_zinc"
zc() { timeout 180 "$JBIN/java" -DZINC_HOME_LIB="$D" -jar "$D/zc.jar" "$@"; }

examples=(hello go_hello top_level go_control go_enums go_assert go_int_literals go_bitwise go_shift go_precedence go_type_casts go_arrays go_sized_arrays go_bool_alias go_numeric_aliases go_interp_escape go_typed_collections go_string_aliases nulls_tuples beam_helpers generic_records generic_functions functions fizzbuzz counter supervised records collections strings trycatch exceptions match protocols lambdas_sam json sealed resources channel multifile fileio filestream pipeline http_client http_facade encoding webauth record_model veneer bools floats)
declare -A want=(
  [hello]='Hello from Zinc on BEAM!'
  [go_hello]=$'Hello, World!\nHello, Zinc!\nThe answer is 42'
  [top_level]=$'doubled(5) = 10\ntriple(5) = 15\nreduce: 60'
  [go_control]=$'fruit: apple\nfruit: banana\nfruit: cherry\ncount: 0\ncount: 1\ncount: 2\nstarting...\nexclusive range 0..3:\n  0\n  1\n  2\ninclusive range 1..=3:\n  1\n  2\n  3\ni=0\ni=1\ni=2\ni=4\ni=5\nControl Flow OK'
  [go_enums]=$'color: Red\ndirection: North\nred!\nEnums OK'
  [go_assert]=$'asserts ok: 5\nstring asserts ok'
  [go_int_literals]=$'hex: 0=0 255=255 DEAD=57005\n0Xff = 255\nbin: 0=0 1=1 2=2 255=255\noct: 0=0 7=7 8=8 493=493\nmixed sum: 765'
  [go_bitwise]=$'a & b = 8\na | b = 14\na ^ b = 6\nflags = 5'
  [go_shift]=$'1 << 4 = 16\n16 >> 2 = 4\nmix = 25'
  [go_precedence]=$'discount: 7.992000000000001\nremainder: 2\nnested: 25'
  [go_type_casts]=$'int(42) = 42\nfloat(42) = 42.0\nlong(42) = 42\nlong(x) = 255\ndouble(3) = 3.0\ndouble(y) = 100.0\nint(3.9) = 3'
  [go_arrays]=$'first: 10\nlength: 5\nsum: 150\nHello, Alice!\nHello, Bob!\nHello, Charlie!\nempty length: 0'
  [go_sized_arrays]=$'int len: 5\nint[0]: 0\nint[2]: 42\nint[4]: 99\nstring len: 3\nnames[0]: zinc\nnames[1]: flow\nbool len: 4\nflags[0]: true\nflags[1]: false'
  [go_bool_alias]=$'ready: true\nlabel: yes\nfalse label: no'
  [go_numeric_aliases]=$'b: 42\nc: 3.5\nd: 4.0\nnums length: 2\nvals second: 4.0'
  [go_interp_escape]=$'greeting: hello\nname: zinc\nalice=95 bob=87\nserver: localhost:8080\ndirect port: 8080'
  [go_typed_collections]=$'names len: 2\nfirst name: alice\nempty nums: 0\nscore sum: 30\nkeys len: 2\nhas alice: true\niter sum: 30\nodds sum: 9'
  [go_string_aliases]=$'  HELLO, ZINC!  \n  hello, zinc!  \n[Hello, Zinc!  ]\n[  Hello, Zinc!]'
  [nulls_tuples]=$'found: vin\nmissing: null\nfound length: 3\nmissing length: null\nfound upper: VIN\nmissing upper: null\npair: 3:beam\nuser: 7:vin\ntyped user: 7:vin\nbundle: 4:beam'
  [beam_helpers]=$'range sum: 6\nclosed sum: 6\nlist slice sum: 90\nbytes slice: BC\nbytes length: 4\nbyte at 2: 67'
  [generic_records]=$'zinc\n10\nbeam:vm'
  [generic_functions]=$'zinc\n8\nvm'
  [functions]=$'7\n42'
  [fizzbuzz]=$'1\n2\nFizz\n4\nBuzz'
  [counter]=7
  [supervised]=$'7\n0\n5'
  [records]=$'7\nvin'
  [collections]=$'13\n13'
  [strings]=$'hello BEAM, n=7\n12\nHELLO, BEAM!\nBEAM\ntrue'
  [trycatch]=$'7\n2'
  [exceptions]=$'8\nno such id\n1\nlocal'
  [match]=$'red\ncool\none\nmany'
  [protocols]=$'hello zinc\nbeam!\nlambda fun'
  [lambdas_sam]=$'true\nn=7\ntrue'
  [json]=$'vin\n41\nsf\n40\n7\nmissing caught'
  [sealed]=$'ok d\nroute big\nfail negative'
  [resources]=$'a.txt\ntxt\n3\ntrue\ntrue\ntrue'
  [channel]=$'5\nx0x1x2x3x4'
  [multifile]=$'[10]\n1'
  [fileio]=$'2\ntrue\n20\n1\ntrue\nno such file or directory: /tmp/zinc_fileio/missing.conf\nfalse'
  [filestream]=$'5\ntrue'
  [pipeline]=$'5\ntrue'
  [http_client]='connect refused caught'
  [http_facade]='facade refused caught'
  [encoding]=$'aGk=\ntrue\n4142\ntrue\ntrue'
  [webauth]=$'true\n64\n64\n36\n0\ntrue'
  [record_model]=$'user\n8\nvin\n2'
  [veneer]=$'13\n3\n5\nlocalhost\n42!'
  [bools]=$'false\ntrue\ntrue\ntrue'
  [floats]=$'3.5\n8.0\n3\n2.5'
)

declare -A wanterr=(
  [return_type]='return_type.zn:2: return: cannot bind a int to String'
  [arg_type]="arg_type.zn:6: add arg 2 ('b'): cannot bind a String to int"
  [app_method]='Application Main can only declare main()'
  [untyped_param]="parameter 'a' needs a type"
  [reassign]='reassign.zn:3: x: cannot bind a String to int'
  [parse_brace]="parse_brace.zn: Parse error: expected '}' but got EOF"
  [typed_collection_value]='typed_collection_value.zn:2: list literal: cannot bind a int to String'
  [tuple_arity]='tuple_arity.zn:6: tuple destructuring: expected 3 values, got 2'
  [lambda_return_type]='lambda_return_type.zn:6: lambda result: cannot bind a int to String'
  [tuple_destructure_type]='tuple_destructure_type.zn:6: id: cannot bind a int to String'
  [byte_index_assign]='byte_index_assign.zn:3: byte[] is binary data and cannot be assigned by index'
  [generic_record_value]="generic_record_value.zn:4: new Pair<String,int> arg 2 ('second'): cannot bind a String to int"
  [generic_function_arg]="generic_function_arg.zn:6: identity arg 1 ('value'): cannot bind a String to int"
)

fail=0

for ex in "${examples[@]}"; do
  src="examples/zinc/$ex.zn"
  [ -d "examples/zinc/$ex" ] && src="examples/zinc/$ex"
  got=$(zc run --once "$src" 2>"$D/$ex.err")
  if [ "$got" = "${want[$ex]}" ]; then
    echo "PASS  $ex  ->  $(printf '%s' "$got" | head -1)"
  else
    echo "FAIL  $ex  ->  got '$got'  want '${want[$ex]}'"
    sed 's/^/    /' "$D/$ex.err"
    fail=1
  fi
done

proj="$D/smoke"
if zc new "$proj" >/dev/null 2>"$D/new.err"; then
  got=$(zc run --once "$proj" 2>"$D/new_run.err")
  if [ "$got" = "Hello from smoke!" ]; then
    echo "PASS  zc-new  ->  $got"
  else
    echo "FAIL  zc-new  ->  got '$got'  want 'Hello from smoke!'"
    sed 's/^/    /' "$D/new_run.err"
    fail=1
  fi
else
  echo "FAIL  zc-new  ->  scaffold failed"
  sed 's/^/    /' "$D/new.err"
  fail=1
fi

for ex in "${!wanterr[@]}"; do
  if zc run --once "examples/zinc_neg/$ex.zn" >/dev/null 2>"$D/neg_$ex.err"; then
    echo "FAIL  neg/$ex  ->  ran, expected error '${wanterr[$ex]}'"
    fail=1
    continue
  fi
  if grep -qF "${wanterr[$ex]}" "$D/neg_$ex.err"; then
    echo "PASS  neg/$ex"
  else
    echo "FAIL  neg/$ex  ->  got '$(head -1 "$D/neg_$ex.err")'  want '${wanterr[$ex]}'"
    fail=1
  fi
done

exit $fail
