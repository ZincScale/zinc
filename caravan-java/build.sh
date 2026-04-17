#!/usr/bin/env bash
# Build caravan-java.jar with plain javac. Dev-only convenience — the full
# `install.sh` handles JDK/sbt-launch bootstrapping end-to-end. Use this
# script when iterating on caravan-java source and you already have JDK 25
# on your PATH.
set -euo pipefail

cd "$(dirname "$0")"

OUT=out
CLASSES=$OUT/classes
JAR=$OUT/caravan-java.jar

rm -rf "$OUT"
mkdir -p "$CLASSES"

javac --release 25 -d "$CLASSES" $(find src/main/java -name '*.java')

(
    cd "$CLASSES"
    jar --create --file "../../$JAR" --main-class caravan.Caravan .
)

echo "Built $JAR"
