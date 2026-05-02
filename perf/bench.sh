#!/bin/bash
# Side-by-side perf check: zinc-emitted Go vs hand-rolled Go on the same workload.
set -e
DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== zinc avro perf ==="
cd "$DIR/avro_zinc"
/home/vrjoshi/.local/bin/zinc-go run .

echo
echo "=== go avro perf ==="
cd "$DIR/avro_go"
go mod tidy 2>&1 | grep -v "^go: " || true
go run .

echo
echo "=== zinc loop perf ==="
cd "$DIR/loop_zinc"
/home/vrjoshi/.local/bin/zinc-go run .

echo
echo "=== go loop perf ==="
cd "$DIR/loop_go"
go run .
