#!/usr/bin/env bash
# ==============================================================================
# SSLE Project 1: Teardown Dual-Subnet Incus Topology for MitM Interception
# ==============================================================================
set -euo pipefail

INTERCEPTOR_NAME="ssle-interceptor"
NET_CLIENT="ssle-net-client"
NET_SERVER="ssle-net-server"

echo "=== Tearing Down MitM Interception Environment ==="

if ! command -v incus >/dev/null 2>&1; then
    echo "[!] 'incus' CLI not found. Nothing to tear down."
    exit 0
fi

if incus info "$INTERCEPTOR_NAME" >/dev/null 2>&1; then
    echo "Stopping and deleting $INTERCEPTOR_NAME..."
    incus stop "$INTERCEPTOR_NAME" --force || true
    incus delete "$INTERCEPTOR_NAME" || true
fi

if incus network show "$NET_CLIENT" >/dev/null 2>&1; then
    echo "Deleting network $NET_CLIENT..."
    incus network delete "$NET_CLIENT" || true
fi

if incus network show "$NET_SERVER" >/dev/null 2>&1; then
    echo "Deleting network $NET_SERVER..."
    incus network delete "$NET_SERVER" || true
fi

echo "=== MitM Environment Teardown Complete ==="
EOF