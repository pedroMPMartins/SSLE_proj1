#!/usr/bin/env bash
set -euo pipefail

CONTAINER_NAME="prometheus-container"
BASE_IMAGE="images:debian/trixie"

echo "=========================================================="
echo "  Deploying Prometheus Monitoring (SSLE Guide 3)"
echo "=========================================================="

# 1. Launch Prometheus container if not running
if incus info "$CONTAINER_NAME" &>/dev/null; then
    echo "==> Container $CONTAINER_NAME already exists."
else
    echo "==> Launching $CONTAINER_NAME with $BASE_IMAGE..."
    incus launch "$BASE_IMAGE" "$CONTAINER_NAME"
    echo "==> Waiting for container network initialization (5s)..."
    sleep 5
fi

# 2. Install Prometheus inside the container
echo "==> Installing prometheus package inside container..."
incus exec "$CONTAINER_NAME" -- apt-get update
incus exec "$CONTAINER_NAME" -- apt-get install -y prometheus

# 3. Detect host bridge IP so Prometheus container can scrape host exporters
HOST_BRIDGE_IP=$(incus network get incusbr0 ipv4.address 2>/dev/null | cut -d'/' -f1 || true)
if [ -z "$HOST_BRIDGE_IP" ]; then
    HOST_BRIDGE_IP="10.0.2.2"
fi
echo "==> Detected host bridge IP: $HOST_BRIDGE_IP"

# 4. Generate container prometheus.yml with dynamic target IP
sed "s/10.0.2.2/$HOST_BRIDGE_IP/g" monitoring/prometheus.yml > /tmp/prometheus_container.yml

echo "==> Pushing prometheus.yml and alerting rules to container..."
incus file push /tmp/prometheus_container.yml "$CONTAINER_NAME/etc/prometheus/prometheus.yml"
rm -f /tmp/prometheus_container.yml

if [ -f "monitoring/prometheus_alerts.yml" ]; then
    incus file push monitoring/prometheus_alerts.yml "$CONTAINER_NAME/etc/prometheus/prometheus_alerts.yml"
fi

# 5. Restart Prometheus to apply new configuration
echo "==> Restarting prometheus service inside $CONTAINER_NAME..."
incus exec "$CONTAINER_NAME" -- systemctl restart prometheus

# 6. Retrieve container IP
CONTAINER_IP=$(incus list "$CONTAINER_NAME" --format csv -c 4 | awk '{print $1}')
echo ""
echo "=========================================================="
echo "  Prometheus is successfully running!"
echo "  Container IP:    $CONTAINER_IP"
echo "  Prometheus Web:  http://${CONTAINER_IP}:9090"
echo "  Targets URL:     http://${CONTAINER_IP}:9090/targets"
echo "=========================================================="
echo ""
echo "To start the Health Exporter (Go binary), run:"
echo "  ./bin/health_exporter &"
echo ""
echo "To start the Diagnostics Exporter (Go binary), run:"
echo "  ./bin/diagnostics_exporter &"
echo "=========================================================="
