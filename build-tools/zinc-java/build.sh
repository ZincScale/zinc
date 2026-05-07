#!/usr/bin/env bash
# Build zinc-java.jar with plain javac. Dev-only convenience — the full
# `install.sh` handles JDK/sbt-launch bootstrapping end-to-end. Use this
# script when iterating on zinc-java source and you already have JDK 25
# on your PATH.
set -euo pipefail

cd "$(dirname "$0")"

OUT=out
CLASSES=$OUT/classes
JAR=$OUT/zinc-java.jar

rm -rf "$OUT"
mkdir -p "$CLASSES"

javac --release 25 -d "$CLASSES" $(find src/main/java -name '*.java')

(
    cd "$CLASSES"
    jar --create --file "../../$JAR" --main-class zinc.Zinc .
)

echo "Built $JAR"
