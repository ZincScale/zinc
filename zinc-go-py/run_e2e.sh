#!/usr/bin/env bash
# Zinc → Go e2e runner (Python transpiler variant).
#
# Single-file examples: transpile → build → run → diff against expected/.
# Mirrors zinc-go/run_e2e.sh where feasible.
set -u

SCRIPT_DIR="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
ZINC="$SCRIPT_DIR/bin/zinc"
TMP_ROOT="$(mktemp -d -t zinc-e2e-XXXXXX)"
trap '/bin/rm -rf "$TMP_ROOT"' EXIT

pass=0 ; fail=0 ; failures=()

run_single() {
    local zn="$1"
    local name="$(basename "${zn%.zn}")"
    local expected="$SCRIPT_DIR/expected/$name.txt"
    local workdir="$TMP_ROOT/$name"
    mkdir -p "$workdir"
    cp "$zn" "$workdir/"

    local actual
    actual="$( "$ZINC" run "$workdir/$(basename "$zn")" 2>&1 )"
    local rc=$?
    # Strip the build-noise lines emitted by zinc build (prefixed with "  ")
    actual="$(printf '%s\n' "$actual" | grep -v '^  /tmp\|^  Built:')"

    if [ -f "$expected" ]; then
        local want="$(cat "$expected")"
        if [ "$actual" = "$want" ]; then
            pass=$((pass+1)); printf 'PASS: %s\n' "$name"
        else
            fail=$((fail+1)); failures+=("$name")
            printf 'FAIL: %s\n' "$name"
            printf '  rc=%d\n' "$rc"
            diff <(printf '%s\n' "$actual") <(printf '%s\n' "$want") | head -10 | sed 's/^/    /'
        fi
    else
        # No expected output file — consider non-zero exit a failure.
        if [ $rc -eq 0 ]; then
            pass=$((pass+1)); printf 'PASS: %s (no expected)\n' "$name"
        else
            fail=$((fail+1)); failures+=("$name")
            printf 'FAIL: %s (rc=%d)\n' "$name" "$rc"
        fi
    fi
}

for zn in "$SCRIPT_DIR"/examples/*.zn; do
    [ -f "$zn" ] || continue
    run_single "$zn"
done

echo
echo "Results: $pass passed, $fail failed"
if [ $fail -gt 0 ]; then
    printf 'Failed: %s\n' "${failures[*]}"
    exit 1
fi
