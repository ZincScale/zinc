#!/usr/bin/env bash
# E2E tests for caravan-csharp build tool
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOOL="$SCRIPT_DIR/../build-tool/caravan-csharp"
PASS=0
FAIL=0

run_test() {
    local name="$1"
    local test_dir="$SCRIPT_DIR/e2e/$name"
    local expected="$SCRIPT_DIR/e2e/expected/$name.txt"

    echo -n "  $name: "

    # Build
    cd "$test_dir"
    "$TOOL" clean >/dev/null 2>&1 || true
    if ! "$TOOL" build >/dev/null 2>&1; then
        echo "FAIL (build failed)"
        FAIL=$((FAIL + 1))
        return
    fi

    # Run and check output
    local output
    output=$("$test_dir/build/"* 2>&1 | head -1)
    local expected_line
    expected_line=$(head -1 "$expected")

    if [[ "$output" == *"$expected_line"* ]]; then
        echo "PASS"
        PASS=$((PASS + 1))
    else
        echo "FAIL (expected '$expected_line', got '$output')"
        FAIL=$((FAIL + 1))
    fi

    "$TOOL" clean >/dev/null 2>&1 || true
}

echo "=== caravan-csharp e2e tests ==="
run_test "hello"
echo ""
echo "=== $PASS passed, $FAIL failed ==="
exit $FAIL
