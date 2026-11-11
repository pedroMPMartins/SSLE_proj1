#!/usr/bin/env bash
# ==============================================================================
# SSLE Practical Guide 5: Teardown DDoS Client Containers
# ==============================================================================
set -euo pipefail

CLIENTS=("load-client-a" "load-client-b")

echo "==> Cleaning up DDoS load client containers..."
for client in "${CLIENTS[@]}"; do
    if incus info "$client" &>/dev/null; then
        incus stop -f "$client" 2>/dev/null || true
        incus delete "$client" 2>/dev/null || true
        echo "Deleted $client."
    else
        echo "$client does not exist."
    fi
done
echo "==> Cleanup complete."
