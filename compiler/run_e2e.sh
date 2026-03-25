#!/bin/bash
# E2E test runner for the Java-based Zinc compiler
# Uses `zinc run` to compile and execute each .zn file, compares output

DIR="$(cd "$(dirname "$0")" && pwd)"
ZINC_DIR="$DIR/../examples/v3"
EXPECTED_DIR="$DIR/expected"
JP="$HOME/.cache/coursier/v1/https/repo1.maven.org/maven2/com/github/javaparser/javaparser-core/3.28.0/javaparser-core-3.28.0.jar"
PASS=0
FAIL=0
SKIP=0
ERRORS=""

for zn in "$ZINC_DIR"/*.zn; do
    name=$(basename "$zn" .zn)

    # Use Java compiler expected files, fall back to Go compiler's
    expected="$EXPECTED_DIR/${name}.txt"
    if [ ! -f "$expected" ]; then
        expected="$ZINC_DIR/expected/${name}.txt"
    fi

    if [ ! -f "$expected" ]; then
        echo "SKIP: $name (no expected output)"
        SKIP=$((SKIP + 1))
        continue
    fi

    # Run via zinc run
    actual=$(java --enable-preview -cp "$DIR/out:$JP" zinc.compiler.Main run "$zn" 2>/dev/null)
    exit_code=$?

    if [ $exit_code -ne 0 ]; then
        # Retry with stderr to show error
        err=$(java --enable-preview -cp "$DIR/out:$JP" zinc.compiler.Main run "$zn" 2>&1 >/dev/null)
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
