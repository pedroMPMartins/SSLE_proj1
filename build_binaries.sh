#!/usr/bin/env bash
set -euo pipefail

echo "==> Compiling static Go binaries into ./bin/..."
mkdir -p bin

CGO_ENABLED=0 go build -o bin/registry ./cmd/registry
CGO_ENABLED=0 go build -o bin/storage ./cmd/storage
CGO_ENABLED=0 go build -o bin/ingestion ./cmd/ingestion
CGO_ENABLED=0 go build -o bin/analytics ./cmd/analytics
CGO_ENABLED=0 go build -o bin/temperature ./cmd/temperature
CGO_ENABLED=0 go build -o bin/attacker ./cmd/attacker
CGO_ENABLED=0 go build -o bin/health_exporter ./cmd/health_exporter
CGO_ENABLED=0 go build -o bin/diagnostics_exporter ./cmd/diagnostics_exporter
CGO_ENABLED=0 go build -o bin/pushgateway_publisher ./cmd/pushgateway_publisher

echo "==> Build complete:"
ls -lh bin/
