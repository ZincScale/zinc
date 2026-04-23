#!/usr/bin/env bash
# Fast runner for a subset of e2e tests. Usage:
#   ./run_sel.sh collections middleware strings
# Runs only the named tests from examples/, diffing against expected/.
set -u

SCRIPT_DIR="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
ZINC="$SCRIPT_DIR/bin/zinc"
TMP_ROOT="$(mktemp -d -t zinc-sel-XXXXXX)"
trap '/bin/rm -rf "$TMP_ROOT"' EXIT

pass=0; fail=0; failures=()

for name in "$@"; do
    zn="$SCRIPT_DIR/examples/${name}.zn"
    [ -f "$zn" ] || { echo "missing: $zn"; continue; }
    expected="$SCRIPT_DIR/expected/${name}.txt"
    workdir="$TMP_ROOT/$name"
    mkdir -p "$workdir"
    cp "$zn" "$workdir/"

    actual="$( "$ZINC" run "$workdir/$(basename "$zn")" 2>&1 )"
    rc=$?
    actual="$(printf '%s\n' "$actual" | grep -v '^  /tmp\|^  Built:')"

    if [ -f "$expected" ]; then
        want="$(cat "$expected")"
        if [ "$actual" = "$want" ]; then
            pass=$((pass+1)); printf 'PASS: %s\n' "$name"
        else
            fail=$((fail+1)); failures+=("$name")
            printf 'FAIL: %s (rc=%d)\n' "$name" "$rc"
            diff <(printf '%s\n' "$actual") <(printf '%s\n' "$want") | head -6 | sed 's/^/    /'
        fi
    else
        printf 'PASS: %s (no expected)\n' "$name"
    fi
done

echo "Selected: $pass passed, $fail failed"
