#!/usr/bin/env bash
set -euo pipefail

echo "==> Compiling static Go binaries into ./bin/..."
mkdir -p bin

CGO_ENABLED=0 go build -o bin/registry ./cmd/registry
CGO_ENABLED=0 go build -o bin/storage ./cmd/storage
CGO_ENABLED=0 go build -o bin/ingestion ./cmd/ingestion
CGO_ENABLED=0 go build -o bin/analytics ./cmd/analytics
CGO_ENABLED=0 go build -o bin/temperature ./cmd/temperature

echo "==> Build complete:"
ls -lh bin/
