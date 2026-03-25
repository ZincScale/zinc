#!/bin/bash
# E2E test runner for the Java-based Zinc compiler
# Compiles each .zn file, compiles the generated Java, runs it, compares output

DIR="$(cd "$(dirname "$0")" && pwd)"
ZINC_DIR="$DIR/../examples/v3"
JP="$HOME/.cache/coursier/v1/https/repo1.maven.org/maven2/com/github/javaparser/javaparser-core/3.28.0/javaparser-core-3.28.0.jar"
PASS=0
FAIL=0
SKIP=0
ERRORS=""

for zn in "$ZINC_DIR"/*.zn; do
    name=$(basename "$zn" .zn)
    expected="$ZINC_DIR/expected/${name}.txt"

    if [ ! -f "$expected" ]; then
        echo "SKIP: $name (no expected output)"
        SKIP=$((SKIP + 1))
        continue
    fi

    # Clean output dir
    outdir="/tmp/zinc-java-e2e-$name"
    rm -rf "$outdir" 2>/dev/null
    mkdir -p "$outdir"

    # Step 1: Zinc → Java
    compile_out=$(java --enable-preview -cp "$DIR/out:$JP" zinc.compiler.Main "$zn" -o "$outdir" 2>&1)
    if [ $? -ne 0 ]; then
        echo "FAIL: $name (zinc compile: $compile_out)"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS\n  $name: zinc compile failed"
        continue
    fi

    # Step 2: Java → class
    java_files=$(find "$outdir" -name "*.java")
    if [ -z "$java_files" ]; then
        echo "FAIL: $name (no .java files generated)"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS\n  $name: no java output"
        continue
    fi

    javac_out=$(javac --enable-preview --source 25 -d "$outdir" $java_files 2>&1)
    if [ $? -ne 0 ]; then
        echo "FAIL: $name (javac: $javac_out)"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS\n  $name: javac failed"
        continue
    fi

    # Determine main class name
    class_name=$(echo "$name" | sed 's/_\([a-z]\)/\U\1/g' | sed 's/^\([a-z]\)/\U\1/')

    # Step 3: Run
    actual=$(java --enable-preview -cp "$outdir" "$class_name" 2>/dev/null)
    exit_code=$?

    if [ $exit_code -ne 0 ]; then
        echo "FAIL: $name (runtime exit code $exit_code)"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS\n  $name: runtime error (exit $exit_code)"
        continue
    fi

    # Step 4: Compare
    expected_content=$(cat "$expected")
    if [ "$actual" = "$expected_content" ]; then
        echo "PASS: $name"
        PASS=$((PASS + 1))
    else
        echo "FAIL: $name (output mismatch)"
        FAIL=$((FAIL + 1))
        ERRORS="$ERRORS\n  $name: output mismatch"
        echo "  expected: $(echo "$expected_content" | head -3)"
        echo "  actual:   $(echo "$actual" | head -3)"
    fi
done

echo ""
echo "Results: $PASS passed, $FAIL failed, $SKIP skipped"
if [ -n "$ERRORS" ]; then
    echo -e "Failures:$ERRORS"
fi
