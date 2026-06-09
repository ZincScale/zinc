#!/usr/bin/env bash
# beam-lab harness — runs Erlang lowering cases & benchmarks in the cached erlang:slim image.
# No local Erlang / no sudo needed.
#
#   ./run.sh lowering/control_flow.erl      # run a single escript
#   ./run.sh bench/bench_loops.erl          # run a benchmark escript
#   ./run.sh -m lowering/otp 'otp_demo:main()'   # compile all .erl in a dir, run an entry expr
#
set -euo pipefail
IMG=erlang:slim
LAB="$(cd "$(dirname "$0")" && pwd)"

if [ "${1:-}" = "-m" ]; then
  dir="$2"; entry="$3"
  # multi-module: copy modules to a writable tmp inside the container, erlc, run entry.
  docker run --rm -v "$LAB:/b:ro" "$IMG" sh -c \
    "mkdir -p /w && cp /b/$dir/*.erl /w/ && cd /w && erlc *.erl && \
     erl -noshell -pa /w -eval '${entry}, init:stop().'"
else
  f="${1:?usage: run.sh <file.erl> | run.sh -m <dir> '<Mod:Fun()>'}"
  docker run --rm -v "$LAB:/b:ro" "$IMG" escript "/b/$f"
fi
