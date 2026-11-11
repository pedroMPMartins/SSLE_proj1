#!/usr/bin/env bash
set -euo pipefail

CONTAINER_NAME="prometheus-container"

echo "==> Cleaning up Prometheus monitoring container..."
if incus info "$CONTAINER_NAME" &>/dev/null; then
    incus stop -f "$CONTAINER_NAME" 2>/dev/null || true
    incus delete "$CONTAINER_NAME" 2>/dev/null || true
    echo "Container $CONTAINER_NAME stopped and removed."
else
    echo "Container $CONTAINER_NAME does not exist. Nothing to clean."
fi
