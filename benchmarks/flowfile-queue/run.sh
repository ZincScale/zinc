#!/bin/bash
# Run FlowFile Queue Throughput benchmarks — Python 3.14t (free-threaded) vs .NET 10
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PYTHON="${PYTHON:-$HOME/python3.14t/bin/python3.14t}"
DOTNET="${DOTNET:-$HOME/.dotnet/dotnet}"

echo "=========================================="
echo " FlowFile Queue Throughput Benchmark"
echo "=========================================="
echo ""

echo ">>> Python 3.14t (free-threaded, no GIL)"
echo "------------------------------------------"
"$PYTHON" "$SCRIPT_DIR/python/queue_bench.py"

echo ""
echo ""
echo ">>> .NET 10 (System.Threading.Channels)"
echo "------------------------------------------"
cd "$SCRIPT_DIR/dotnet"
"$DOTNET" run -c Release --no-restore 2>/dev/null || "$DOTNET" run -c Release
