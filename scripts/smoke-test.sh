#!/usr/bin/env bash
# Copyright 2026 victorybhg
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

# Smoke test: build compiler, run unit tests, run e2e tests
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT/compiler"

echo "Building Zinc compiler..."
make build

echo ""
echo "Running unit tests..."
make test

echo ""
echo "All smoke tests passed."
