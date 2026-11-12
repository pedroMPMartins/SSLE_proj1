#!/usr/bin/env bash
# ==============================================================================
# SSLE Project 1: Dual-Subnet Incus Topology for MitM Interception (Guide 5 - Threat 3)
# Topology:
#   [ssle-sensor-client] (10.20.1.50)
#            |
#       (ssle-net-client: 10.20.1.0/24)
#            |
#   [ssle-interceptor] (Gateway / Router: 10.20.1.1 on eth0, 10.20.2.1 on eth1)
#            |
#       (ssle-net-server: 10.20.2.0/24)
#            |
#   [ssle-ingestion-srv] (10.20.2.10)
# ==============================================================================
set -euo pipefail

INTERCEPTOR_NAME="ssle-interceptor"
NET_CLIENT="ssle-net-client"
NET_SERVER="ssle-net-server"

echo "=== [1/5] Checking Incus Availability ==="
if ! command -v incus >/dev/null 2>&1; then
    echo "[!] Warning: 'incus' CLI not found on this host."
    echo "[*] For local host development, verify that incus is installed and in PATH."
    echo "[*] Demonstration architecture configuration is documented in docs/guide5_mitm_and_cryptography.md"
    exit 0
fi

echo "=== [2/5] Creating Isolated Subnet Bridges ==="
# Network 1: Client Subnet (10.20.1.0/24)
if ! incus network show "$NET_CLIENT" >/dev/null 2>&1; then
    incus network create "$NET_CLIENT" \
        ipv4.address=10.20.1.1/24 \
        ipv4.nat=false \
        ipv6.address=none
    echo "Created network $NET_CLIENT"
fi

# Network 2: Server Subnet (10.20.2.0/24)
if ! incus network show "$NET_SERVER" >/dev/null 2>&1; then
    incus network create "$NET_SERVER" \
        ipv4.address=10.20.2.1/24 \
        ipv4.nat=true \
        ipv6.address=none
    echo "Created network $NET_SERVER"
fi

echo "=== [3/5] Launching Interceptor Gateway Container ==="
if ! incus info "$INTERCEPTOR_NAME" >/dev/null 2>&1; then
    incus launch images:ubuntu/24.04 "$INTERCEPTOR_NAME" --network "$NET_CLIENT"
    incus network attach "$NET_SERVER" "$INTERCEPTOR_NAME" eth1
    echo "Launched $INTERCEPTOR_NAME with dual network interfaces."
fi

echo "=== [4/5] Configuring IP Forwarding & Interception Routing ==="
# Enable IPv4 forwarding on the interceptor
incus exec "$INTERCEPTOR_NAME" -- sysctl -w net.ipv4.ip_forward=1

# Install iptables, curl, and mitmproxy inside interceptor
incus exec "$INTERCEPTOR_NAME" -- bash -c "DEBIAN_FRONTEND=noninteractive apt-get update -y && apt-get install -y iptables curl mitmproxy"

echo "=== [5/5] Ready for MitM Demonstration ==="
echo ""
echo "To activate transparent interception on the router container, execute:"
echo "  incus exec $INTERCEPTOR_NAME -- iptables -t nat -A PREROUTING -p tcp --dport 8080 -j REDIRECT --to-ports 8080"
echo "  incus exec $INTERCEPTOR_NAME -- mitmproxy --mode transparent --listen-port 8080"
echo ""
echo "Without HMAC/TLS: Traffic can be modified in-flight."
echo "With HMAC-SHA256: Ingestion server rejects tampered payload with HTTP 401 Unauthorized."
echo "With TLS/mTLS: Interceptor fails TLS handshake with untrusted certificate error."
EOF