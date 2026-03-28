#!/bin/bash
# E2E test runner for zinc (Python-based transpiler)
DIR="$(cd "$(dirname "$0")" && pwd)"
ZINC="$DIR/../zinc.py"
E2E_DIR="$DIR/e2e"
PASS=0
FAIL=0

for zn in "$E2E_DIR"/*.zn; do
    name=$(basename "$zn" .zn)
    expected="$E2E_DIR/expected/${name}.txt"

    if [ ! -f "$expected" ]; then
        echo "SKIP: $name (no expected output)"
        continue
    fi

    actual=$(python3 "$ZINC" run "$zn" 2>&1)
    exp=$(cat "$expected")

    if [ "$actual" = "$exp" ]; then
        echo "PASS: $name"
        PASS=$((PASS + 1))
    else
        echo "FAIL: $name"
        echo "  expected: $(echo "$exp" | head -3)"
        echo "  got:      $(echo "$actual" | head -3)"
        FAIL=$((FAIL + 1))
    fi
done

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ $FAIL -eq 0 ] || exit 1
