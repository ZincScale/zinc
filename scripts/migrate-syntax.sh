#!/bin/bash
# Migrate Zinc syntax to simplified form using the Go migration tool.
# Usage: ./scripts/migrate-syntax.sh [--dry-run]
#
# This script builds the Go migration tool and runs it on all target files:
# - .zn example files (full file transform)
# - Go test files (transforms Zinc inside backtick strings only)
# - CLI templates (main.go, repl.go)

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TOOL="$PROJECT_DIR/scripts/migrate-syntax-bin"

# Build the migration tool
echo "Building migration tool..."
go build -o "$TOOL" "$SCRIPT_DIR/migrate-syntax.go"

DRY_RUN=""
if [ "$1" = "--dry-run" ] || [ "$1" = "-n" ]; then
    DRY_RUN="--dry-run"
    echo "DRY RUN — no files will be modified"
fi

echo ""
echo "Migrating test files..."
"$TOOL" $DRY_RUN \
    "$PROJECT_DIR/internal/codegen/codegen_test.go" \
    "$PROJECT_DIR/internal/codegen/e2e_test.go" \
    "$PROJECT_DIR/internal/codegen/integration_test.go" \
    "$PROJECT_DIR/internal/typechecker/typechecker_test.go" \
    "$PROJECT_DIR/internal/typechecker/integration_test.go" \
    "$PROJECT_DIR/internal/project/project_test.go"

echo ""
echo "Migrating example files..."
find "$PROJECT_DIR/examples" -name "*.zn" -print0 | xargs -0 "$TOOL" $DRY_RUN

echo ""
echo "Migrating CLI templates..."
"$TOOL" $DRY_RUN \
    "$PROJECT_DIR/cmd/zinc/main.go" \
    "$PROJECT_DIR/cmd/zinc/repl.go"

# Cleanup
rm -f "$TOOL"

echo ""
echo "Done. Run 'go test ./...' to verify after updating the parser."
