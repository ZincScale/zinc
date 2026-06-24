#!/usr/bin/env bash
# zc end-to-end: init a project, zc run it on BEAM, then zc test (EUnit underneath):
# a passing suite (actor spawn + Assert.fails on a crash) exits 0, a failing one exits 1.
set -euo pipefail
cd "$(dirname "$0")"

REBAR3="${REBAR3:-$HOME/.cache/zinc/rebar3}"
if [ ! -x "$REBAR3" ]; then
  mkdir -p "$(dirname "$REBAR3")"
  curl -sSfL -o "$REBAR3" https://github.com/erlang/rebar3/releases/latest/download/rebar3
  chmod +x "$REBAR3"
fi
JAVA_DIR="${JAVA_DIR:-$HOME/.local/java/current}"
ROOT="$(cd .. && pwd)"

run_in_docker() {
  docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -v "$ROOT":/work -v "$JAVA_DIR":/java -v "$REBAR3":/usr/local/bin/rebar3 \
    -e PATH="/java/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin" \
    -w /tmp erlang:slim sh -c "$1"
}

got=$(run_in_docker '/work/bin/zc init demo 1>&2 && cd demo && /work/bin/zc run' | tail -1)

if [ "$got" = "Hello from demo!" ]; then
  echo "PASS  zc run  ->  $got"
else
  echo "FAIL  zc run  ->  got '$got'  want 'Hello from demo!'"
  exit 1
fi

# zc new is canonical-only: obsolete scaffold flags must fail, not silently create a
# different project shape.
flag_check='
set +e
out=$(/work/bin/zc init --template old 2>&1)
status=$?
set -e
if [ $status -ne 0 ] && echo "$out" | grep -q "usage: zc init <name>"; then echo FLAG-OK; fi
'
got=$(run_in_docker "$flag_check" | tail -1)
if [ "$got" = "FLAG-OK" ]; then
  echo "PASS  zc init rejects old flags"
else
  echo "FAIL  zc init flag rejection  ->  got '$got'  want 'FLAG-OK'"
  exit 1
fi

# -- zc fmt + doctor: project tree formatting is canonical-source-only and skips generated dirs --
fmt_doctor='
/work/bin/zc init demo >/dev/null 2>&1 && cd demo && mkdir -p test _build src/zinc_gen
cat > src/main.zn <<EOF
void main() {
print("x")
}
EOF
cat > test/main_test.zn <<EOF
class MainTest : Test {
void ok() {
Assert.equals(1, 1)
}
}
EOF
cat > _build/ignored.zn <<EOF
void ignored() {
print("build")
}
EOF
cat > src/zinc_gen/ignored.zn <<EOF
void ignored() {
print("generated")
}
EOF
/work/bin/zc fmt . >/tmp/fmt.out
/work/bin/zc doctor >/tmp/doctor.out
if grep -q "  print(\"x\")" src/main.zn \
  && grep -q "  void ok()" test/main_test.zn \
  && grep -q "^print(\"build\")" _build/ignored.zn \
  && grep -q "^print(\"generated\")" src/zinc_gen/ignored.zn \
  && grep -q "sources    src 1 .zn, test 1 .zn" /tmp/doctor.out; then
  echo FMT-DOCTOR-OK
fi
'
got=$(run_in_docker "$fmt_doctor" | tail -1)
if [ "$got" = "FMT-DOCTOR-OK" ]; then
  echo "PASS  zc fmt/doctor"
else
  echo "FAIL  zc fmt/doctor  ->  got '$got'  want 'FMT-DOCTOR-OK'"
  exit 1
fi

# -- zc test: green suite passes (exit 0), red suite fails (exit 1) --
fixture='
/work/bin/zc init demo 1>&2 && cd demo && mkdir -p test
cat > src/counter.zn <<EOF
class Counter : Actor {
  int count = 0

  void incr() {
    count = count + 1
  }

  int get() {
    return count
  }

  int divideBy(int n) {
    return count / n
  }
}
EOF
cat > test/counter_test.zn <<EOF
class CounterTest : Test {
  void counts() {
    var c = Counter()
    c.incr()
    c.incr()
    Assert.equals(2, c.get())
  }

  void crashObservableAtTheCall() {
    var c = Counter()
    Assert.fails(() -> c.divideBy(0))
  }
}
EOF
# assert on the COUNT, not just the exit code: a discovery bug that runs zero
# tests also exits 0 (this bit us once — fresh projects ran 0 tests)
green=$(/work/bin/zc test 2>&1); status=$?
echo "$green" 1>&2
if [ $status -eq 0 ] && echo "$green" | grep -q "2 tests, 0 failures"; then echo GREEN-OK; fi
cat > test/counter_test.zn <<EOF
class CounterTest : Test {
  void wrongOnPurpose() {
    var c = Counter()
    Assert.equals(5, c.get())
  }
}
EOF
red=$(/work/bin/zc test 2>&1); rstatus=$?
echo "$red" 1>&2
# failures must cite the .zn source line (assert source maps), and exit 1
if [ $rstatus -ne 0 ] && echo "$red" | grep -q "counter_test.zn:"; then echo RED-OK; fi
'
got=$(run_in_docker "$fixture" | tail -2 | tr '\n' ' ')

if [ "$got" = "GREEN-OK RED-OK " ]; then
  echo "PASS  zc test"
else
  echo "FAIL  zc test  ->  got '$got'  want 'GREEN-OK RED-OK '"
  exit 1
fi
