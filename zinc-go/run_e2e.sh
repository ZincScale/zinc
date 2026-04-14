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

# --- Negative tests (must fail to compile) ---
# examples-fail/<name>.zn + expected/<name>.txt: zinc should exit non-zero,
# and stderr must contain the expected error text (substring match).
FAIL_DIR="$DIR/examples-fail"
if [ -d "$FAIL_DIR" ]; then
    for zn in "$FAIL_DIR"/*.zn; do
        [ -f "$zn" ] || continue
        name=$(basename "$zn" .zn)
        expected="$EXPECTED_DIR/${name}.txt"

        if [ ! -f "$expected" ]; then
            echo "SKIP: $name (fail) (no expected output)"
            SKIP=$((SKIP + 1))
            continue
        fi

        actual=$("$ZINC_BIN" build "$zn" 2>&1)
        rc=$?
        expected_text=$(cat "$expected")

        if [ $rc -eq 0 ]; then
            echo "FAIL: $name (fail) — expected compile error but build succeeded"
            FAIL=$((FAIL + 1))
            ERRORS="$ERRORS\n  $name (fail)"
            continue
        fi

        if echo "$actual" | grep -qF "$expected_text"; then
            echo "PASS: $name (fail)"
            PASS=$((PASS + 1))
        else
            echo "FAIL: $name (fail) — error message didn't match expected substring"
            echo "  Expected to contain: $expected_text"
            echo "  Actual:              $(echo "$actual" | head -3)"
            FAIL=$((FAIL + 1))
            ERRORS="$ERRORS\n  $name (fail)"
        fi
    done
fi

# --- zinc test regression ---
# examples-test/<name>/ is a project exercising `zinc test`. We assert
# go test ran (PASS lines or overall FAIL) matches expected/<name>.txt
# substring. Exit code: 0 if expected says "pass", 1 if it says "fail".
TEST_DIR="$DIR/examples-test"
if [ -d "$TEST_DIR" ]; then
    for projdir in "$TEST_DIR"/*/; do
        [ -d "$projdir" ] || continue
        name=$(basename "$projdir")
        expected="$EXPECTED_DIR/${name}.txt"

        if [ ! -f "$expected" ]; then
            echo "SKIP: $name (test) (no expected output)"
            SKIP=$((SKIP + 1))
            continue
        fi

        actual=$("$ZINC_BIN" test "$projdir" 2>&1)
        expected_text=$(cat "$expected")

        if echo "$actual" | grep -qF "$expected_text"; then
            echo "PASS: $name (test)"
            PASS=$((PASS + 1))
        else
            echo "FAIL: $name (test) — output didn't contain expected"
            echo "  Expected to contain: $(head -1 "$expected")"
            echo "  Actual (last 5):     $(echo "$actual" | tail -5 | tr '\n' ' ')"
            FAIL=$((FAIL + 1))
            ERRORS="$ERRORS\n  $name (test)"
        fi
    done
fi

echo ""
echo "Results: $PASS passed, $FAIL failed, $SKIP skipped"
if [ -n "$ERRORS" ]; then
    echo -e "Failed:$ERRORS"
fi
exit $FAIL
