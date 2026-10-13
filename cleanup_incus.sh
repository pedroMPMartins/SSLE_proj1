#!/usr/bin/env bash
set -uo pipefail

SERVICES=("ssle-registry" "ssle-storage" "ssle-ingestion" "ssle-analytics" "ssle-temperature")

echo "==> Stopping and deleting SSLE Incus containers..."
for svc in "${SERVICES[@]}"; do
    if incus info "$svc" &>/dev/null; then
        echo "Deleting $svc..."
        incus delete -f "$svc"
    fi
done

echo "==> Cleaning up shared data..."
sudo rm -rf /var/lib/ssle-data

echo "==> Cleanup complete."
