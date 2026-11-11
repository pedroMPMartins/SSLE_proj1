#!/usr/bin/env bash
set -euo pipefail

BASE_IMAGE="images:debian/trixie"
SHARED_HOST_DIR="/var/lib/ssle-data"

echo "==> 1. Ensuring binaries are compiled..."
if [ ! -d "bin" ] || [ ! -f "bin/registry" ]; then
    ./build_binaries.sh
fi

echo "==> 2. Preparing shared database directory on host ($SHARED_HOST_DIR)..."
sudo mkdir -p "$SHARED_HOST_DIR"
sudo chmod 777 "$SHARED_HOST_DIR"

SERVICES=("ssle-registry" "ssle-storage" "ssle-ingestion" "ssle-analytics" "ssle-temperature")

echo "==> 3. Launching containers..."
for svc in "${SERVICES[@]}"; do
    if incus info "$svc" &>/dev/null; then
        echo "Container $svc already exists. Skipping launch."
    else
        echo "Launching $svc using $BASE_IMAGE..."
        incus launch "$BASE_IMAGE" "$svc"
    fi
done

echo "==> Waiting 5 seconds for containers to initialize..."
sleep 5

echo "==> 4. Mounting shared storage for storage and analytics..."
# Add disk device if not already attached
if ! incus config device show ssle-storage | grep -q "shared-db"; then
    incus config device add ssle-storage shared-db disk source="$SHARED_HOST_DIR" path=/data
fi
if ! incus config device show ssle-analytics | grep -q "shared-db"; then
    incus config device add ssle-analytics shared-db disk source="$SHARED_HOST_DIR" path=/data
fi

echo "==> 5. Pushing binaries..."
incus file push bin/registry ssle-registry/usr/local/bin/registry --mode 0755
incus file push bin/storage ssle-storage/usr/local/bin/storage --mode 0755
incus file push bin/ingestion ssle-ingestion/usr/local/bin/ingestion --mode 0755
incus file push bin/analytics ssle-analytics/usr/local/bin/analytics --mode 0755
incus file push bin/temperature ssle-temperature/usr/local/bin/temperature --mode 0755

echo "==> 6. Installing systemd units..."
incus exec ssle-registry -- bash -c 'cat <<EOF > /etc/systemd/system/ssle-registry.service
[Unit]
Description=SSLE Service Registry
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/registry
Restart=always

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now ssle-registry
'

# --- Storage Service ---
incus exec ssle-storage -- bash -c 'cat <<EOF > /etc/systemd/system/ssle-storage.service
[Unit]
Description=SSLE Storage Service
After=network.target

[Service]
Type=simple
WorkingDirectory=/data
Environment="PORT=5002"
Environment="SERVICE_URL=http://ssle-storage.incus:5002"
Environment="REGISTRY_URL=http://ssle-registry.incus:5050/register"
ExecStart=/usr/local/bin/storage
Restart=always

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now ssle-storage
'

# --- Ingestion Service ---
incus exec ssle-ingestion -- mkdir -p /var/log/ssle
incus exec ssle-ingestion -- bash -c 'cat <<EOF > /etc/systemd/system/ssle-ingestion.service
[Unit]
Description=SSLE Ingestion Service
After=network.target

[Service]
Type=simple
Environment="PORT=5001"
Environment="SERVICE_URL=http://ssle-ingestion.incus:5001"
Environment="REGISTRY_URL=http://ssle-registry.incus:5050"
Environment="ACCESS_LOG_PATH=/var/log/ssle/ingestion_access.log"
ExecStart=/usr/local/bin/ingestion
Restart=always

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now ssle-ingestion
'

# --- Analytics Service ---
incus exec ssle-analytics -- bash -c 'cat <<EOF > /etc/systemd/system/ssle-analytics.service
[Unit]
Description=SSLE Analytics Service
After=network.target

[Service]
Type=simple
WorkingDirectory=/data
Environment="PORT=5003"
Environment="SERVICE_URL=http://ssle-analytics.incus:5003"
Environment="REGISTRY_URL=http://ssle-registry.incus:5050"
Environment="DB_PATH=/data/temp_data.db"
ExecStart=/usr/local/bin/analytics
Restart=always

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now ssle-analytics
'

# --- Temperature Sensor ---
incus exec ssle-temperature -- bash -c 'cat <<EOF > /etc/systemd/system/ssle-temperature.service
[Unit]
Description=SSLE Temperature Sensor
After=network.target

[Service]
Type=simple
Environment="REGISTRY_URL=http://ssle-registry.incus:5050"
ExecStart=/usr/local/bin/temperature
Restart=always

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now ssle-temperature
'

echo "==> 7. All services started. Letting temperature sensor send readings (waiting 10s)..."
sleep 10

echo -e "\n==> 8. Verifying Deployment:"
echo -e "\n--- Container Status ---"
incus list ssle-

echo -e "\n--- Registered Services (queried from ssle-registry) ---"
incus exec ssle-registry -- curl -s http://localhost:5050/services | jq .

echo -e "\n\n--- Analytics Average (queried from ssle-analytics) ---"
incus exec ssle-analytics -- curl -s http://localhost:5003/average | jq .
echo ""
