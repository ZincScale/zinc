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

# -- zc test: green suite passes (exit 0), red suite fails (exit 1) --
fixture='
/work/bin/zc init demo 1>&2 && cd demo && mkdir -p test
cat > src/Counter.zinc <<EOF
class Counter implements Actor {
  int count = 0;
  void incr()         { count = count + 1; }
  int get()           { return count; }
  int divideBy(int n) { return count / n; }
}
EOF
cat > test/CounterTest.zinc <<EOF
class CounterTest implements Test {
  public void counts() {
    var c = new Counter();
    c.incr();
    c.incr();
    Assert.equals(2, c.get());
  }

  public void crashObservableAtTheCall() {
    var c = new Counter();
    Assert.fails(() -> c.divideBy(0));
  }
}
EOF
/work/bin/zc test 1>&2 && echo GREEN-OK
cat > test/CounterTest.zinc <<EOF
class CounterTest implements Test {
  public void wrongOnPurpose() {
    var c = new Counter();
    Assert.equals(5, c.get());
  }
}
EOF
if /work/bin/zc test 1>&2; then echo RED-NOT-CAUGHT; else echo RED-OK; fi
'
got=$(run_in_docker "$fixture" | tail -2 | tr '\n' ' ')

if [ "$got" = "GREEN-OK RED-OK " ]; then
  echo "PASS  zc test"
else
  echo "FAIL  zc test  ->  got '$got'  want 'GREEN-OK RED-OK '"
  exit 1
fi
