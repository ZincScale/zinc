#!/bin/bash
# E2E test runner — compares actual output against expected output.
# Usage: ./examples/v3/run_tests.sh [path-to-zinc-binary]

ZINC="${1:-zinc}"
DIR="$(cd "$(dirname "$0")" && pwd)"
PASS=0
FAIL=0
ERRORS=""

for zn in "$DIR"/*.zn; do
    name=$(basename "$zn" .zn)
    expected="$DIR/expected/${name}.txt"

    if [ ! -f "$expected" ]; then
        echo "SKIP: $name (no expected output)"
        continue
    fi

    actual=$("$ZINC" run "$zn" 2>/dev/null)
    exit_code=$?

    if [ $exit_code -ne 0 ]; then
        echo "FAIL: $name (exit code $exit_code)"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS\n  $name: non-zero exit code $exit_code"
        continue
    fi

    expected_content=$(cat "$expected")
    if [ "$actual" = "$expected_content" ]; then
        echo "PASS: $name"
        PASS=$((PASS + 1))
    else
        echo "FAIL: $name (output mismatch)"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS\n  $name: output differs"
        diff <(echo "$expected_content") <(echo "$actual") | head -20
    fi
done

echo ""
echo "Results: $PASS passed, $FAIL failed"
if [ $FAIL -gt 0 ]; then
    echo -e "Failures:$ERRORS"
    exit 1
fi
