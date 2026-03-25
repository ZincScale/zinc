#!/bin/bash
# E2E test runner — uses the zinc compiler to build and run each .zn file
# Tests the full toolchain: zinc run (transpile → javac → execute)

DIR="$(cd "$(dirname "$0")" && pwd)"
ZINC="$DIR/zinc"
ZINC_DIR="$DIR/../examples/v3"
EXPECTED_DIR="$DIR/expected"
PASS=0
FAIL=0
SKIP=0
ERRORS=""

if [ ! -x "$ZINC" ]; then
    echo "error: zinc launcher not found at $ZINC"
    echo "run 'make package' or 'make build' first"
    exit 1
fi

for zn in "$ZINC_DIR"/*.zn; do
    name=$(basename "$zn" .zn)

    expected="$EXPECTED_DIR/${name}.txt"
    if [ ! -f "$expected" ]; then
        expected="$ZINC_DIR/expected/${name}.txt"
    fi

    if [ ! -f "$expected" ]; then
        echo "SKIP: $name (no expected output)"
        SKIP=$((SKIP + 1))
        continue
    fi

    # Run via zinc compiler
    actual=$("$ZINC" run "$zn" 2>/dev/null)
    exit_code=$?

    if [ $exit_code -ne 0 ]; then
        err=$("$ZINC" run "$zn" 2>&1 >/dev/null)
        echo "FAIL: $name (exit code $exit_code: $err)"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS\n  $name: exit $exit_code"
        continue
    fi

    expected_content=$(cat "$expected")
    if [ "$actual" = "$expected_content" ]; then
        echo "PASS: $name"
        PASS=$((PASS + 1))
    else
        echo "FAIL: $name (output mismatch)"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS\n  $name: output mismatch"
        echo "  expected: $(echo "$expected_content" | head -3)"
        echo "  actual:   $(echo "$actual" | head -3)"
    fi
done

echo ""
echo "Results: $PASS passed, $FAIL failed, $SKIP skipped"
if [ -n "$ERRORS" ]; then
    echo -e "Failures:$ERRORS"
fi
