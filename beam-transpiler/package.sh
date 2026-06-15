#!/usr/bin/env bash
# Build the release jar: zc CLI + the zinc transpiler in one self-contained zc.jar
# (Main-Class: Zc). Then assemble dist/zc-<ver>/ with the rebar_zinc plugin + bin/zc
# shim, and tar it as the GitHub-release artifact install.sh downloads.
set -euo pipefail
cd "$(dirname "$0")"

VER="${1:-0.1.0}"
JBIN="$HOME/.local/java/current/bin"; [ -x "$JBIN/javac" ] || JBIN="$(dirname "$(command -v javac)")"
DIST="dist"; STAGE="$DIST/zc-$VER"
rm -rf "$DIST/classes" "$STAGE"; mkdir -p "$DIST/classes" "$STAGE/lib" "$STAGE/bin"

# compile the transpiler (zinc.*) and the CLI (Zc, default package) together
"$JBIN/javac" -d "$DIST/classes" $(find src/zinc -name '*.java') zc/Zc.java

# one jar, Main-Class Zc; zc invokes the transpiler in-jar as `java -cp zc.jar zinc.Main`
echo "Main-Class: Zc" > "$DIST/manifest.txt"
"$JBIN/jar" cfm "$STAGE/lib/zc.jar" "$DIST/manifest.txt" -C "$DIST/classes" .

# rebar_zinc plugin rides along (project builds shell out to the transpiler via the jar)
cp -r rebar_zinc "$STAGE/lib/rebar_zinc"

# bin/zc shim: prefer the managed JRE, run the jar; ZINC_HOME = lib (so the rebar plugin
# finds zc.jar for project builds)
cat > "$STAGE/bin/zc" <<'SHIM'
#!/usr/bin/env bash
LIB="$(cd "$(dirname "$0")/../lib" && pwd)"
JAVA="${ZINC_JAVA:-}"
if [ -z "$JAVA" ] && [ -x "${ZC_HOME:-$HOME/.zc}/jre/bin/java" ]; then
  JAVA="${ZC_HOME:-$HOME/.zc}/jre/bin/java"
fi
exec "${JAVA:-java}" -DZINC_HOME_LIB="$LIB" -jar "$LIB/zc.jar" "$@"
SHIM
chmod +x "$STAGE/bin/zc"

( cd "$DIST" && tar czf "zc-$VER.tar.gz" "zc-$VER" )
echo "built $DIST/zc-$VER.tar.gz"
echo "  lib/zc.jar  lib/rebar_zinc/  bin/zc"
