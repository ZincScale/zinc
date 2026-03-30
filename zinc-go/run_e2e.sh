#!/bin/bash
# E2E test runner for the Go-based Zinc compiler
# Transpiles each .zn file, compiles and runs the generated Go, compares output

DIR="$(cd "$(dirname "$0")" && pwd)"
ZN_DIR="$DIR/examples"
EXPECTED_DIR="$DIR/expected"
ZINC="$DIR/cmd/zinc"
PASS=0
FAIL=0
SKIP=0
ERRORS=""

# Build the compiler first
echo "Building zinc compiler..."
cd "$DIR" && go build -o /tmp/zinc-go-bin ./cmd/zinc/ || { echo "FAIL: compiler build failed"; exit 1; }
ZINC_BIN="/tmp/zinc-go-bin"

for zn in "$ZN_DIR"/*.zn; do
    name=$(basename "$zn" .zn)
    expected="$EXPECTED_DIR/${name}.txt"

    if [ ! -f "$expected" ]; then
        echo "SKIP: $name (no expected output)"
        SKIP=$((SKIP + 1))
        continue
    fi

    # Transpile and run
    actual=$("$ZINC_BIN" run "$zn" 2>&1)
    expected_text=$(cat "$expected")

    # Compare sorted output (Go map iteration order is non-deterministic)
    actual_sorted=$(echo "$actual" | sort)
    expected_sorted=$(echo "$expected_text" | sort)

    if [ "$actual_sorted" = "$expected_sorted" ]; then
        echo "PASS: $name"
        PASS=$((PASS + 1))
    else
        echo "FAIL: $name"
        echo "  Expected: $(echo "$expected_text" | head -3)"
        echo "  Actual:   $(echo "$actual" | head -3)"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS\n  $name"
    fi
done

# --- Multi-file project tests ---
for projdir in "$ZN_DIR"/*/; do
    [ -d "$projdir" ] || continue
    name=$(basename "$projdir")
    expected="$EXPECTED_DIR/${name}.txt"

    if [ ! -f "$expected" ]; then
        echo "SKIP: $name (no expected output)"
        SKIP=$((SKIP + 1))
        continue
    fi

    actual=$("$ZINC_BIN" run "$projdir" 2>&1)
    expected_text=$(cat "$expected")

    actual_sorted=$(echo "$actual" | sort)
    expected_sorted=$(echo "$expected_text" | sort)

    if [ "$actual_sorted" = "$expected_sorted" ]; then
        echo "PASS: $name (project)"
        PASS=$((PASS + 1))
    else
        echo "FAIL: $name (project)"
        echo "  Expected: $(echo "$expected_text" | head -3)"
        echo "  Actual:   $(echo "$actual" | head -3)"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS\n  $name"
    fi
done

echo ""
echo "Results: $PASS passed, $FAIL failed, $SKIP skipped"
if [ -n "$ERRORS" ]; then
    echo -e "Failed:$ERRORS"
fi
exit $FAIL
