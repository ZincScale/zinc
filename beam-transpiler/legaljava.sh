#!/usr/bin/env bash
# Legal-Java gate: every .zinc surface (minus the erlang.* FFI basement) must compile
# under javac against the zinc-prelude jar. Catches non-JDK symbols (the ImmutableList bug)
# and any construct that isn't real Java. zinc's default imports (java.util.* + zinc.*) are
# prepended -- the source stays import-free, like Kotlin/Groovy defaults.
set -uo pipefail
cd "$(dirname "$0")"
JBIN="$HOME/.local/java/current/bin"; [ -x "$JBIN/javac" ] || JBIN="$(dirname "$(command -v javac)")"
JAR="zinc-prelude/zinc-prelude.jar"
PRE=$'import java.util.*;\nimport java.util.function.*;\nimport zinc.*;\n'
TMP="out/legaljava"; rm -rf "$TMP"; mkdir -p "$TMP"

pass=0; fail=0; exempt=0; failed=()

# build the prelude jar if missing or stale
if [ ! -f "$JAR" ] || [ -n "$(find zinc-prelude/zinc -name '*.java' -newer "$JAR" 2>/dev/null)" ]; then
  rm -rf zinc-prelude/out && mkdir -p zinc-prelude/out
  "$JBIN/javac" -d zinc-prelude/out zinc-prelude/zinc/*.java || { echo "prelude FAILED to compile"; exit 1; }
  "$JBIN/jar" cf "$JAR" -C zinc-prelude/out zinc
fi

# compile one program (all .zinc under $root, recursively) against the prelude.
# package = the file's directory relative to $root (zinc's dir-as-package model),
# so a file in util/ gets `package util;` -- mirrors what javac's layout requires.
# $1 = label, $2 = program root dir.
check() {
  local label="$1" root="$2"
  local files; files=$(find "$root" -name '*.zinc' | sort)
  [ -n "$files" ] || return
  while IFS= read -r f; do
    if grep -q "^import erlang\." "$f"; then echo "EXEMPT-FFI  $label"; exempt=$((exempt+1)); return; fi
  done <<< "$files"
  local d="$TMP/$label"; mkdir -p "$d"
  while IFS= read -r f; do
    local pub; pub=$(grep -oE 'public (class|interface|enum|record) [A-Za-z_][A-Za-z0-9_]*' "$f" | head -1 | awk '{print $3}')
    local base; base=$(basename "$f" .zinc); local name="${pub:-$base}"
    local rel; rel=$(dirname "${f#"$root"/}"); local pkg=""; local sub=""
    if [ "$rel" != "." ]; then pkg="package ${rel//\//.};"$'\n'; sub="$rel/"; mkdir -p "$d/$sub"; fi
    { printf '%s%s' "$pkg" "$PRE"; cat "$f"; } > "$d/$sub$name.java"
  done <<< "$files"
  if err=$("$JBIN/javac" -cp "$JAR" -d "$d/classes" $(find "$d" -name '*.java') 2>&1); then
    echo "PASS  $label"; pass=$((pass+1))
  else
    echo "FAIL  $label"; fail=$((fail+1)); failed+=("$label")
    echo "$err" | sed 's/^/      /' | head -6
  fi
}

# single-file examples (each is its own program)
for f in examples/*.zinc; do [ -f "$f" ] || continue; b=$(basename "$f" .zinc); d="$TMP/one/$b"; mkdir -p "$d"; cp "$f" "$d/"; check "$b" "$d"; done
# multi-file programs
check "multifile" examples/multifile
check "dogfood-webdemo" dogfood/webdemo/src
check "dogfood-sqldemo" dogfood/sqldemo/src

echo "----------------------------------------"
echo "legal-Java gate: PASS=$pass FAIL=$fail EXEMPT-FFI=$exempt"
[ "$fail" -eq 0 ] || { echo "FAILED: ${failed[*]}"; exit 1; }
