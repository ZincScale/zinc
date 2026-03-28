#!/bin/bash
# Python E2E test runner — transpiles .zn → .py, runs with Python, compares output
# Tests the full toolchain: zinc build --python → python execution

DIR="$(cd "$(dirname "$0")" && pwd)"
COMPILER_JAR="$DIR/zinc-compiler.jar"
ZN_DIR="$DIR/../examples/v3"
EXPECTED_DIR="$DIR/test/python/expected"
RUNTIME_DIR="$DIR/test/python"
OUT_DIR="/tmp/zinc-python-e2e"
PASS=0
FAIL=0
SKIP=0
ERRORS=""

# Find Python — prefer 3.14t but accept any 3.10+
PYTHON=""
for candidate in python3.14t python3.14 python3.13 python3.12 python3.11 python3.10 python3 python; do
    if command -v "$candidate" &>/dev/null; then
        PYTHON="$candidate"
        break
    fi
done

if [ -z "$PYTHON" ]; then
    echo "error: no Python 3.10+ found"
    exit 1
fi

echo "using: $PYTHON ($($PYTHON --version 2>&1))"

if [ ! -f "$COMPILER_JAR" ]; then
    echo "error: zinc-compiler.jar not found at $COMPILER_JAR"
    echo "run 'make build' first"
    exit 1
fi

# Clean output dir to avoid stale files shadowing Python stdlib
if [ -d "$OUT_DIR" ]; then
    find "$OUT_DIR" -name "*.py" -delete 2>/dev/null
fi
mkdir -p "$OUT_DIR"

for expected_file in "$EXPECTED_DIR"/*.txt; do
    name=$(basename "$expected_file" .txt)
    zn="$ZN_DIR/${name}.zn"

    if [ ! -f "$zn" ]; then
        echo "SKIP: $name (no .zn source)"
        SKIP=$((SKIP + 1))
        continue
    fi

    # Step 1: Transpile .zn → .py
    transpile_output=$(java -jar "$COMPILER_JAR" build --python "$zn" -o "$OUT_DIR" 2>&1)
    transpile_exit=$?

    if [ $transpile_exit -ne 0 ]; then
        echo "FAIL: $name (transpilation failed)"
        ERRORS="$ERRORS\n--- $name (transpile) ---\n$transpile_output"
        FAIL=$((FAIL + 1))
        continue
    fi

    # Find the generated .py file in app/ subdirectory
    APP_DIR="$OUT_DIR/app"
    py_file="$APP_DIR/${name}.py"
    if [ ! -f "$py_file" ]; then
        # Try without underscores (error_handling → errorhandling)
        py_name=$(echo "$name" | tr -d '_')
        py_file="$APP_DIR/${py_name}.py"
    fi

    if [ ! -f "$py_file" ]; then
        echo "FAIL: $name (no .py output found)"
        ERRORS="$ERRORS\n--- $name ---\nExpected output at $py_file but not found. Files: $(ls $APP_DIR/ 2>/dev/null)"
        FAIL=$((FAIL + 1))
        continue
    fi

    # Step 2: Run as module from parent dir (avoids stdlib shadowing)
    module_name="app.$(basename "$py_file" .py)"
    actual=$(cd "$OUT_DIR" && $PYTHON -m "$module_name" 2>&1)
    run_exit=$?

    if [ $run_exit -ne 0 ]; then
        echo "FAIL: $name (runtime error, exit=$run_exit)"
        ERRORS="$ERRORS\n--- $name (runtime) ---\n$actual"
        FAIL=$((FAIL + 1))
        continue
    fi

    # Step 3: Compare output
    expected=$(cat "$expected_file")

    if [ "$actual" = "$expected" ]; then
        echo "PASS: $name"
        PASS=$((PASS + 1))
    else
        echo "FAIL: $name (output mismatch)"
        ERRORS="$ERRORS\n--- $name (output) ---\nExpected:\n$expected\n\nActual:\n$actual"
        FAIL=$((FAIL + 1))
    fi
done

echo ""
echo "Results: $PASS passed, $FAIL failed, $SKIP skipped"

if [ -n "$ERRORS" ]; then
    echo ""
    echo "=== Failures ==="
    echo -e "$ERRORS"
fi

exit $FAIL
