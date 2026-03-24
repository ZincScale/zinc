#!/bin/bash
# Multi-file actor project integration test
# Usage: ./run_test.sh [path-to-zinc-binary]

ZINC="${1:-zinc}"
DIR="$(cd "$(dirname "$0")" && pwd)"

# Build from the project directory
echo "Building multi-file actor project..."
build_output=$(cd "$DIR" && "$ZINC" build src/ 2>&1)
build_exit=$?
if [ $build_exit -ne 0 ]; then
    echo "FAIL: build failed (exit $build_exit)"
    echo "$build_output"
    exit 1
fi
echo "Build OK"

# Run and capture output (mill run writes to stdout, warnings to stderr)
actual=$(cd "$DIR" && "$ZINC" run src/main.zn 2>/dev/null)
run_exit=$?
if [ $run_exit -ne 0 ]; then
    echo "FAIL: run failed (exit $run_exit)"
    exit 1
fi

expected="counter: 10
hi world
after reset: 0
supervised: 1, 2
team shutdown
Multi-file actors OK"

if [ "$actual" = "$expected" ]; then
    echo "PASS: multi-file actor project"
else
    echo "FAIL: output mismatch"
    echo "Expected:"
    echo "$expected"
    echo "Actual:"
    echo "$actual"
    exit 1
fi
