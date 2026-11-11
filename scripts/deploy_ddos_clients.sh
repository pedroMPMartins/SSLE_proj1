#!/usr/bin/env bash
# ==============================================================================
# SSLE Practical Guide 5: Deploy Multi-Client Botnet Harness (load-client-a & b)
# ==============================================================================
set -euo pipefail

BASE_IMAGE="images:debian/trixie"
CLIENTS=("load-client-a" "load-client-b")

echo "=========================================================="
echo "  Deploying Distributed DDoS Client Containers (Guide 5)"
echo "  Clients: ${CLIENTS[*]}"
echo "=========================================================="

for client in "${CLIENTS[@]}"; do
    if incus info "$client" &>/dev/null; then
        echo "==> Container $client already exists. Skipping launch."
    else
        echo "==> Launching $client using $BASE_IMAGE..."
        incus launch "$BASE_IMAGE" "$client"
    fi
done

echo "==> Waiting for container network initialization (5s)..."
sleep 5

for client in "${CLIENTS[@]}"; do
    echo "==> Provisioning benchmarking tools on $client..."
    incus exec "$client" -- bash -c '
        apt-get update
        apt-get install -y apache2-utils curl jq
        cat << "EOF" > /root/payload.json
{"device_id":"botnet-probe","temperature":23.5}
EOF
    '
    CLIENT_IP=$(incus list "$client" --format csv -c 4 | awk '{print $1}')
    echo "==> $client ready at IP: $CLIENT_IP"
done

echo "=========================================================="
echo "  Both DDoS load clients successfully provisioned!"
echo "  You can now execute concurrent ApacheBench runs:"
echo "    ./scripts/run_ddos_benchmark.sh"
echo "=========================================================="
