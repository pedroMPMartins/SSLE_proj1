#!/usr/bin/env bash
# ==============================================================================
# SSLE Practical Guide 4: Cleanup Wazuh SIEM Container
# ==============================================================================
set -euo pipefail

CONTAINER_NAME="wazuh-container"

echo "==> Cleaning up Wazuh SIEM container..."
if incus info "$CONTAINER_NAME" &>/dev/null; then
    incus stop -f "$CONTAINER_NAME" 2>/dev/null || true
    incus delete "$CONTAINER_NAME" 2>/dev/null || true
    echo "Container $CONTAINER_NAME stopped and removed."
else
    echo "Container $CONTAINER_NAME does not exist. Nothing to clean."
fi
