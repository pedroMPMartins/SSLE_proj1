#!/usr/bin/env bash
# ==============================================================================
# SSLE Project 1: Master Incus Infrastructure Deployment Orchestrator
# Provisions the entire 7-tier SOA fabric + Prometheus Monitoring + Wazuh SIEM
# in isolated Linux Containers (Incus) with automated configuration and verification.
# ==============================================================================
set -euo pipefail

# ------------------------------------------------------------------------------
# Terminal Formatting & Colors
# ------------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m' # No Color

# ------------------------------------------------------------------------------
# Default Configuration
# ------------------------------------------------------------------------------
BASE_IMAGE="${BASE_IMAGE:-images:ubuntu/22.04}"
SHARED_HOST_DIR="${SHARED_HOST_DIR:-/var/lib/ssle-data}"
HMAC_SECRET="ssle-demo-shared-secret-key-32b"
ADMIN_TOKEN="ssle-admin-secret-token-2026"

DEPLOY_MONITORING=true
DEPLOY_WAZUH=true

# Parse CLI Flags
for arg in "$@"; do
    case "$arg" in
        --skip-monitoring)
            DEPLOY_MONITORING=false
            ;;
        --skip-wazuh)
            DEPLOY_WAZUH=false
            ;;
        --help|-h)
            echo -e "${BOLD}SSLE Project 1: Complete Incus Infrastructure Orchestrator${NC}"
            echo -e "Usage: ./deploy.sh [OPTIONS]\n"
            echo -e "Options:"
            echo -e "  --skip-monitoring   Skip Prometheus monitoring container deployment"
            echo -e "  --skip-wazuh        Skip Wazuh SIEM manager & agent deployment"
            echo -e "  -h, --help          Show this help message and exit\n"
            echo -e "Environment Overrides:"
            echo -e "  BASE_IMAGE          Base Incus image (default: images:ubuntu/22.04)"
            echo -e "  SHARED_HOST_DIR     Shared database host path (default: /var/lib/ssle-data)"
            exit 0
            ;;
    esac
done

# ------------------------------------------------------------------------------
# Helper Log Functions
# ------------------------------------------------------------------------------
log_banner() {
    echo -e "\n${CYAN}${BOLD}================================================================================${NC}"
    echo -e "${CYAN}${BOLD}  $1${NC}"
    echo -e "${CYAN}${BOLD}================================================================================${NC}\n"
}

log_info() {
    echo -e " ${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e " ${GREEN}${BOLD}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e " ${YELLOW}${BOLD}[WARNING]${NC} $1"
}

log_error() {
    echo -e " ${RED}${BOLD}[ERROR]${NC} $1"
}

# ------------------------------------------------------------------------------
# Pre-Flight Verification
# ------------------------------------------------------------------------------
log_banner "SSLE Project 1: Complete Incus Deployment"

log_info "Performing pre-flight tooling verification..."
if ! command -v incus &>/dev/null; then
    log_error "Command 'incus' not found!"
    echo -e "\n${YELLOW}Note: Incus is required to run the containerized deployment.${NC}"
    echo -e "If you are running in a restricted sandbox or local development machine,"
    echo -e "you can run the full system demonstration directly via:"
    echo -e "  ${BOLD}./run.sh${NC} (or ./test_runner.sh)\n"
    exit 1
fi

if ! incus info &>/dev/null; then
    log_error "Incus daemon is not reachable or your user lacks permissions."
    echo -e "Please ensure your user is in the 'incus-admin' group or run with sudo."
    exit 1
fi
log_success "Incus daemon is healthy and accessible."

# Ensure binaries are compiled
log_info "Ensuring all static Go binaries are compiled in ./bin/..."
if [ ! -d "bin" ] || [ ! -f "bin/registry" ] || [ ! -f "bin/health_exporter" ]; then
    ./build_binaries.sh
fi
log_success "All static Go binaries compiled successfully."

# ------------------------------------------------------------------------------
# Step 1: Prepare Shared Host Storage for SQLite
# ------------------------------------------------------------------------------
log_banner "Step 1: Preparing Shared Host Storage"
log_info "Creating shared directory on host: ${SHARED_HOST_DIR}"
if [ -d "$SHARED_HOST_DIR" ]; then
    sudo chmod 777 "$SHARED_HOST_DIR" 2>/dev/null || chmod 777 "$SHARED_HOST_DIR" 2>/dev/null || true
else
    sudo mkdir -p "$SHARED_HOST_DIR" 2>/dev/null || mkdir -p "$SHARED_HOST_DIR"
    sudo chmod 777 "$SHARED_HOST_DIR" 2>/dev/null || chmod 777 "$SHARED_HOST_DIR"
fi
log_success "Shared database host directory ready."

# ------------------------------------------------------------------------------
# Step 2: Launch SOA Microservice Containers
# ------------------------------------------------------------------------------
log_banner "Step 2: Launching Incus Microservice Containers"

CORE_SERVICES=(
    "ssle-registry"
    "ssle-storage"
    "ssle-ingestion"
    "ssle-analytics"
    "ssle-temperature"
    "ssle-exporter"
)

for svc in "${CORE_SERVICES[@]}"; do
    if incus info "$svc" &>/dev/null; then
        log_info "Container '${svc}' already exists. Skipping launch."
    else
        log_info "Launching container '${svc}' using image '${BASE_IMAGE}'..."
        incus launch "$BASE_IMAGE" "$svc"
    fi
done

log_info "Waiting 6 seconds for container network initialization..."
sleep 6

# ------------------------------------------------------------------------------
# Step 3: Attach Shared Disk Devices
# ------------------------------------------------------------------------------
log_banner "Step 3: Configuring Shared Disk Mounts"

if ! incus config device show ssle-storage 2>/dev/null | grep -q "shared-db"; then
    log_info "Attaching shared-db device to ssle-storage (/data)..."
    incus config device add ssle-storage shared-db disk source="$SHARED_HOST_DIR" path=/data
else
    log_info "Device 'shared-db' already attached to ssle-storage."
fi

if ! incus config device show ssle-analytics 2>/dev/null | grep -q "shared-db"; then
    log_info "Attaching shared-db device to ssle-analytics (/data)..."
    incus config device add ssle-analytics shared-db disk source="$SHARED_HOST_DIR" path=/data
else
    log_info "Device 'shared-db' already attached to ssle-analytics."
fi
log_success "Shared database volumes mounted at /data."

# ------------------------------------------------------------------------------
# Step 4: Push Compiled Binaries to Containers
# ------------------------------------------------------------------------------
log_banner "Step 4: Pushing Compiled Static Binaries"

log_info "Pushing binary 'registry' -> ssle-registry..."
incus file push bin/registry ssle-registry/usr/local/bin/registry --mode 0755

log_info "Pushing binary 'storage' -> ssle-storage..."
incus file push bin/storage ssle-storage/usr/local/bin/storage --mode 0755

log_info "Pushing binary 'ingestion' & 'attacker' -> ssle-ingestion..."
incus file push bin/ingestion ssle-ingestion/usr/local/bin/ingestion --mode 0755
incus file push bin/attacker ssle-ingestion/usr/local/bin/attacker --mode 0755 2>/dev/null || true

log_info "Pushing binary 'analytics' -> ssle-analytics..."
incus file push bin/analytics ssle-analytics/usr/local/bin/analytics --mode 0755

log_info "Pushing binary 'temperature' -> ssle-temperature..."
incus file push bin/temperature ssle-temperature/usr/local/bin/temperature --mode 0755

log_info "Pushing monitoring exporter binaries -> ssle-exporter..."
incus file push bin/health_exporter ssle-exporter/usr/local/bin/health_exporter --mode 0755
incus file push bin/diagnostics_exporter ssle-exporter/usr/local/bin/diagnostics_exporter --mode 0755

log_success "All binaries pushed successfully."

# ------------------------------------------------------------------------------
# Step 5: Provision and Start Systemd Services
# ------------------------------------------------------------------------------
log_banner "Step 5: Installing and Starting Systemd Service Units"

# --- 1. Service Registry (:5050) ---
log_info "Configuring systemd service for ssle-registry..."
incus exec ssle-registry -- mkdir -p /var/log/ssle
incus exec ssle-registry -- bash -c 'cat << "EOF" > /etc/systemd/system/ssle-registry.service
[Unit]
Description=SSLE Service Registry with Hierarchical Discovery & FQSN
After=network.target

[Service]
Type=simple
Environment="PORT=5050"
Environment="ADMIN_TOKEN=ssle-admin-secret-token-2026"
Environment="ACCESS_LOG_PATH=/var/log/ssle/registry_access.log"
Environment="ENABLE_RATE_LIMIT=true"
Environment="RATE_LIMIT_RPS=30.0"
Environment="REGION=pt-aveiro"
Environment="ZONE=lab-ssle"
Environment="PHYSICAL_HOST=vm-node-1"
Environment="CONTAINER_NAME=incus:ssle-registry"
ExecStart=/usr/local/bin/registry
Restart=always
RestartSec=2s

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now ssle-registry
'

# --- 2. Storage Service (:5002) ---
log_info "Configuring systemd service for ssle-storage..."
incus exec ssle-storage -- mkdir -p /var/log/ssle
incus exec ssle-storage -- bash -c 'cat << "EOF" > /etc/systemd/system/ssle-storage.service
[Unit]
Description=SSLE SQLite Storage Service
After=network.target

[Service]
Type=simple
WorkingDirectory=/data
Environment="PORT=5002"
Environment="SERVICE_URL=http://ssle-storage.incus:5002"
Environment="REGISTRY_URL=http://ssle-registry.incus:5050/register"
Environment="DB_PATH=/data/temp_data.db"
Environment="ACCESS_LOG_PATH=/var/log/ssle/storage_access.log"
Environment="ENABLE_RATE_LIMIT=true"
Environment="RATE_LIMIT_RPS=30.0"
Environment="REGION=pt-aveiro"
Environment="ZONE=lab-ssle"
Environment="PHYSICAL_HOST=vm-node-1"
Environment="CONTAINER_NAME=incus:ssle-storage"
ExecStart=/usr/local/bin/storage
Restart=always
RestartSec=2s

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now ssle-storage
'

# --- 3. Ingestion Gateway (:5001) ---
log_info "Configuring systemd service for ssle-ingestion (Hardened Gateway)..."
incus exec ssle-ingestion -- mkdir -p /var/log/ssle
incus exec ssle-ingestion -- bash -c 'cat << "EOF" > /etc/systemd/system/ssle-ingestion.service
[Unit]
Description=SSLE Hardened Ingestion Gateway with SIEM Logging
After=network.target

[Service]
Type=simple
Environment="PORT=5001"
Environment="SERVICE_URL=http://ssle-ingestion.incus:5001"
Environment="REGISTRY_URL=http://ssle-registry.incus:5050"
Environment="STORAGE_URL=http://ssle-storage.incus:5002"
Environment="ACCESS_LOG_PATH=/var/log/ssle/ingestion_access.log"
Environment="ENABLE_RATE_LIMIT=true"
Environment="RATE_LIMIT_RPS=15.0"
Environment="ENABLE_INPUT_VALIDATION=true"
Environment="MIN_TEMP=-50.0"
Environment="MAX_TEMP=100.0"
Environment="BLOCKED_DEVICES=malicious-bot-99"
Environment="ENABLE_SHELLSHOCK_DEFENSE=true"
Environment="ENABLE_HMAC_VERIFICATION=true"
Environment="HMAC_SECRET_KEY=ssle-demo-shared-secret-key-32b"
Environment="MAX_TIMESTAMP_DRIFT_SECS=10"
Environment="REGION=pt-aveiro"
Environment="ZONE=lab-ssle"
Environment="PHYSICAL_HOST=vm-node-1"
Environment="CONTAINER_NAME=incus:ssle-ingestion"
ExecStart=/usr/local/bin/ingestion
Restart=always
RestartSec=2s

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now ssle-ingestion
'

# --- 4. Analytics Service (:5003) ---
log_info "Configuring systemd service for ssle-analytics..."
incus exec ssle-analytics -- mkdir -p /var/log/ssle
incus exec ssle-analytics -- bash -c 'cat << "EOF" > /etc/systemd/system/ssle-analytics.service
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
Environment="ACCESS_LOG_PATH=/var/log/ssle/analytics_access.log"
Environment="ENABLE_RATE_LIMIT=true"
Environment="RATE_LIMIT_RPS=30.0"
Environment="REGION=pt-aveiro"
Environment="ZONE=lab-ssle"
Environment="PHYSICAL_HOST=vm-node-1"
Environment="CONTAINER_NAME=incus:ssle-analytics"
ExecStart=/usr/local/bin/analytics
Restart=always
RestartSec=2s

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now ssle-analytics
'

# --- 5. Temperature Sensor (:5000) ---
log_info "Configuring systemd service for ssle-temperature (HMAC Signer)..."
incus exec ssle-temperature -- bash -c 'cat << "EOF" > /etc/systemd/system/ssle-temperature.service
[Unit]
Description=SSLE Edge Temperature Sensor
After=network.target

[Service]
Type=simple
Environment="PORT=5000"
Environment="SERVICE_URL=http://ssle-temperature.incus:5000"
Environment="REGISTRY_URL=http://ssle-registry.incus:5050"
Environment="INGEST_URL=http://ssle-ingestion.incus:5001"
Environment="HMAC_SECRET_KEY=ssle-demo-shared-secret-key-32b"
Environment="REGION=pt-aveiro"
Environment="ZONE=lab-ssle"
Environment="PHYSICAL_HOST=vm-node-1"
Environment="CONTAINER_NAME=incus:ssle-temperature"
ExecStart=/usr/local/bin/temperature
Restart=always
RestartSec=2s

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now ssle-temperature
'

# --- 6. Monitoring Exporters (:8000 & :8001) ---
log_info "Configuring systemd services for ssle-exporter (Health & Diagnostics)..."
incus exec ssle-exporter -- bash -c '
cat << "EOF" > /etc/systemd/system/ssle-health-exporter.service
[Unit]
Description=SSLE Prometheus Health Exporter
After=network.target

[Service]
Type=simple
Environment="PORT=8000"
Environment="REGISTRY_URL=http://ssle-registry.incus:5050"
ExecStart=/usr/local/bin/health_exporter
Restart=always
RestartSec=2s

[Install]
WantedBy=multi-user.target
EOF

cat << "EOF" > /etc/systemd/system/ssle-diagnostics-exporter.service
[Unit]
Description=SSLE Prometheus Diagnostics Exporter
After=network.target

[Service]
Type=simple
Environment="PORT=8001"
Environment="INGESTION_URL=http://ssle-ingestion.incus:5001"
ExecStart=/usr/local/bin/diagnostics_exporter
Restart=always
RestartSec=2s

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now ssle-health-exporter
systemctl enable --now ssle-diagnostics-exporter
'
log_success "All 6 microservice and exporter systemd units installed and running."

# ------------------------------------------------------------------------------
# Step 6: Deploy Prometheus Monitoring Container
# ------------------------------------------------------------------------------
if [ "$DEPLOY_MONITORING" = true ]; then
    log_banner "Step 6: Deploying Prometheus Monitoring Server"
    PROM_CONTAINER="prometheus-container"

    if incus info "$PROM_CONTAINER" &>/dev/null; then
        log_info "Container '${PROM_CONTAINER}' already exists."
    else
        log_info "Launching container '${PROM_CONTAINER}'..."
        incus launch "$BASE_IMAGE" "$PROM_CONTAINER"
        sleep 5
    fi

    log_info "Checking Prometheus installation inside ${PROM_CONTAINER}..."
    if ! incus exec "$PROM_CONTAINER" -- which prometheus &>/dev/null; then
        log_info "Installing prometheus package..."
        incus exec "$PROM_CONTAINER" -- apt-get update
        incus exec "$PROM_CONTAINER" -- apt-get install -y prometheus
    fi

    log_info "Pushing Prometheus configuration and alerting rules..."
    incus file push monitoring/prometheus.yml "${PROM_CONTAINER}/etc/prometheus/prometheus.yml"
    if [ -f "monitoring/prometheus_alerts.yml" ]; then
        incus file push monitoring/prometheus_alerts.yml "${PROM_CONTAINER}/etc/prometheus/prometheus_alerts.yml"
    fi

    incus exec "$PROM_CONTAINER" -- systemctl restart prometheus
    log_success "Prometheus monitoring deployed and active."
else
    log_warn "Skipping Prometheus monitoring deployment (--skip-monitoring set)."
fi

# ------------------------------------------------------------------------------
# Step 7: Deploy Wazuh SIEM Manager
# ------------------------------------------------------------------------------
if [ "$DEPLOY_WAZUH" = true ]; then
    log_banner "Step 7: Deploying Wazuh SIEM Manager (Guide 4)"
    WAZUH_CONTAINER="wazuh-container"

    if incus info "$WAZUH_CONTAINER" &>/dev/null; then
        log_info "Container '${WAZUH_CONTAINER}' already exists."
    else
        log_info "Launching '${WAZUH_CONTAINER}' with resource limits (2 CPUs, 2GiB RAM)..."
        incus launch images:ubuntu/22.04 "$WAZUH_CONTAINER" \
            -c limits.cpu=2 \
            -c limits.memory=2GiB
        sleep 8
    fi

    log_info "Checking Wazuh Manager installation inside ${WAZUH_CONTAINER}..."
    if ! incus exec "$WAZUH_CONTAINER" -- which wazuh-control &>/dev/null; then
        log_info "Installing wazuh-manager from official repository..."
        incus exec "$WAZUH_CONTAINER" -- bash -c '
            apt-get update && apt-get install -y curl apt-transport-https lsb-release gnupg2
            curl -s https://packages.wazuh.com/key/GPG-KEY-WAZUH | gpg --no-default-keyring --keyring gnupg-ring:/usr/share/keyrings/wazuh.gpg --import && chmod 644 /usr/share/keyrings/wazuh.gpg
            echo "deb [signed-by=/usr/share/keyrings/wazuh.gpg] https://packages.wazuh.com/4.x/apt/ stable main" > /etc/apt/sources.list.d/wazuh.list
            apt-get update
            apt-get install -y wazuh-manager python3
        '
    fi

    log_info "Pushing custom detection rules, decoders, and IP reputation list..."
    incus file push monitoring/wazuh/rules/local_rules.xml "${WAZUH_CONTAINER}/var/ossec/etc/rules/local_rules.xml"
    incus file push monitoring/wazuh/decoders/local_decoder.xml "${WAZUH_CONTAINER}/var/ossec/etc/decoders/local_decoder.xml"

    incus exec "$WAZUH_CONTAINER" -- mkdir -p /var/ossec/etc/lists
    incus file push monitoring/wazuh/lists/alienvault_reputation "${WAZUH_CONTAINER}/var/ossec/etc/lists/alienvault_reputation"

    log_info "Configuring ruleset list and Active Response inside ossec.conf..."
    incus exec "$WAZUH_CONTAINER" -- python3 -c '
import re

conf_path = "/var/ossec/etc/ossec.conf"
with open(conf_path, "r") as f:
    content = f.read()

# 1. Declare AlienVault reputation list inside <ruleset>
if "alienvault_reputation" not in content and "</ruleset>" in content:
    content = content.replace("</ruleset>", "    <list>etc/lists/alienvault_reputation</list>\n  </ruleset>")

# 2. Insert or replace Active Response for Cluster-Wide Global SIEM Quarantine
ar_block = """  <!-- SSLE Active Response for Cluster-Wide Global SIEM Quarantine -->
  <active-response>
    <command>firewall-drop</command>
    <location>all</location>
    <rules_id>100100, 100101, 100102, 100104, 100106, 100108</rules_id>
    <timeout>60</timeout>
  </active-response>"""

if re.search(r"<active-response>[\s\S]*?<command>firewall-drop</command>[\s\S]*?</active-response>", content):
    content = re.sub(r"<active-response>[\s\S]*?<command>firewall-drop</command>[\s\S]*?</active-response>", ar_block, content)
elif "</ossec_config>" in content:
    content = content.replace("</ossec_config>", ar_block + "\n</ossec_config>")

with open(conf_path, "w") as f:
    f.write(content)
'

    log_info "Compiling CDB reputation list with wazuh-makelists..."
    incus exec "$WAZUH_CONTAINER" -- bash -c '
        if [ -x /var/ossec/bin/wazuh-makelists ]; then
            /var/ossec/bin/wazuh-makelists
        elif [ -x /var/ossec/bin/ossec-makelists ]; then
            /var/ossec/bin/ossec-makelists
        fi
    '

    log_info "Setting Wazuh ownership and file permissions..."
    incus exec "$WAZUH_CONTAINER" -- bash -c '
        chown -R wazuh:wazuh /var/ossec/etc/ 2>/dev/null || chown -R ossec:ossec /var/ossec/etc/ 2>/dev/null || true
        chmod 640 /var/ossec/etc/rules/local_rules.xml /var/ossec/etc/decoders/local_decoder.xml 2>/dev/null || true
        chmod 640 /var/ossec/etc/lists/alienvault_reputation* 2>/dev/null || true
    '

    log_info "Testing Wazuh Manager configuration (/var/ossec/bin/wazuh-analysisd -t)..."
    incus exec "$WAZUH_CONTAINER" -- /var/ossec/bin/wazuh-analysisd -t

    log_info "Restarting wazuh-manager..."
    incus exec "$WAZUH_CONTAINER" -- systemctl restart wazuh-manager
    log_success "Wazuh SIEM Manager deployed and active."

    # --------------------------------------------------------------------------
    # Step 8: Deploy Wazuh Agents & Kernel Firewall (iptables) across All Core Microservices
    # --------------------------------------------------------------------------
    log_banner "Step 8: Deploying Wazuh Agents & Kernel Firewalls (iptables) across All Core Microservices"
    WAZUH_IP=$(incus list "$WAZUH_CONTAINER" --format csv -c 4 | awk '{print $1}')
    if [ -z "$WAZUH_IP" ]; then
        WAZUH_IP="wazuh-container.incus"
    fi
    log_info "Detected Wazuh Manager address: ${WAZUH_IP}"

    CORE_SERVICES=("ssle-ingestion" "ssle-registry" "ssle-storage" "ssle-analytics")
    declare -A SERVICE_PORTS=(
        ["ssle-ingestion"]="5001"
        ["ssle-registry"]="5050"
        ["ssle-storage"]="5002"
        ["ssle-analytics"]="5003"
    )

    for svc in "${CORE_SERVICES[@]}"; do
        short_name="${svc#ssle-}"
        svc_port="${SERVICE_PORTS[$svc]}"
        log_info "Hardening container '${svc}' (iptables firewall + wazuh-agent + ${short_name}_access.log)..."

        incus exec "$svc" -- bash -c "
            mkdir -p /var/log/ssle
            apt-get update && apt-get install -y curl apt-transport-https lsb-release gnupg2 iptables
            curl -s https://packages.wazuh.com/key/GPG-KEY-WAZUH | gpg --no-default-keyring --keyring gnupg-ring:/usr/share/keyrings/wazuh.gpg --import && chmod 644 /usr/share/keyrings/wazuh.gpg
            echo 'deb [signed-by=/usr/share/keyrings/wazuh.gpg] https://packages.wazuh.com/4.x/apt/ stable main' > /etc/apt/sources.list.d/wazuh.list
            apt-get update
            WAZUH_MANAGER='${WAZUH_IP}' apt-get install -y wazuh-agent

            # Baseline iptables firewall protection: allow loopback, established, and service port
            iptables -C INPUT -i lo -j ACCEPT 2>/dev/null || iptables -A INPUT -i lo -j ACCEPT
            iptables -C INPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT 2>/dev/null || iptables -A INPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
            iptables -C INPUT -p tcp --dport ${svc_port} -j ACCEPT 2>/dev/null || iptables -A INPUT -p tcp --dport ${svc_port} -j ACCEPT
        "

        log_info "Configuring Apache access log ingestion for ${svc} in ossec.conf..."
        incus exec "$svc" -- python3 -c "
conf_path = '/var/ossec/etc/ossec.conf'
with open(conf_path, 'r') as f:
    content = f.read()

localfile_block = '''
  <!-- SSLE ${svc} Apache-Compatible Access Log Ingestion -->
  <localfile>
    <log_format>apache</log_format>
    <location>/var/log/ssle/${short_name}_access.log</location>
  </localfile>
'''

if '/var/log/ssle/${short_name}_access.log' not in content:
    if '</ossec_config>' in content:
        content = content.replace('</ossec_config>', localfile_block + '\n</ossec_config>')
        with open(conf_path, 'w') as f:
            f.write(content)
"

        incus exec "$svc" -- systemctl daemon-reload
        incus exec "$svc" -- systemctl enable --now wazuh-agent
        log_success "Wazuh Agent & iptables firewall active on '${svc}' forwarding /var/log/ssle/${short_name}_access.log."
    done
else
    log_warn "Skipping Wazuh SIEM deployment (--skip-wazuh set)."
fi

# ------------------------------------------------------------------------------
# Step 9: Post-Deployment Verification & System Inspection
# ------------------------------------------------------------------------------
log_banner "Step 9: Post-Deployment Verification & Cluster Status"

log_info "Allowing services to complete initial discovery registration (8s)..."
sleep 8

echo -e "\n${BOLD}=== Incus Container Roster ===${NC}"
incus list "ssle-|prometheus-|wazuh-"

echo -e "\n${BOLD}=== Service Registry Catalog (GET http://ssle-registry.incus:5050/services) ===${NC}"
incus exec ssle-registry -- curl -s http://localhost:5050/services | (command -v jq &>/dev/null && jq . || cat) || true

echo -e "\n${BOLD}=== Analytics Calculated Average (GET http://ssle-analytics.incus:5003/average) ===${NC}"
incus exec ssle-analytics -- curl -s http://localhost:5003/average | (command -v jq &>/dev/null && jq . || cat) || true

echo -e "\n${BOLD}=== Health Exporter Scrape Sample (GET http://ssle-exporter.incus:8000/metrics) ===${NC}"
incus exec ssle-exporter -- curl -s http://localhost:8000/metrics | grep -E '^service_health|^service_response_time' | head -n 10 || true

# ------------------------------------------------------------------------------
# Deployment Summary
# ------------------------------------------------------------------------------
log_banner "SSLE Infrastructure Successfully Deployed!"

PROM_IP=$(incus list prometheus-container --format csv -c 4 2>/dev/null | awk '{print $1}' || echo "N/A")
WAZUH_IP=$(incus list wazuh-container --format csv -c 4 2>/dev/null | awk '{print $1}' || echo "N/A")
INGEST_IP=$(incus list ssle-ingestion --format csv -c 4 2>/dev/null | awk '{print $1}' || echo "N/A")
REG_IP=$(incus list ssle-registry --format csv -c 4 2>/dev/null | awk '{print $1}' || echo "N/A")

printf "%-22s | %-18s | %-12s | %-26s\n" "Container Name" "IPv4 Address" "Port(s)" "Role"
echo "--------------------------------------------------------------------------------"
printf "%-22s | %-18s | %-12s | %-26s\n" "ssle-registry" "$REG_IP" "5050" "FQSN Service Registry"
printf "%-22s | %-18s | %-12s | %-26s\n" "ssle-storage" "$(incus list ssle-storage --format csv -c 4 | awk '{print $1}')" "5002" "SQLite Storage (/data)"
printf "%-22s | %-18s | %-12s | %-26s\n" "ssle-ingestion" "$INGEST_IP" "5001" "Gateway + Wazuh Agent"
printf "%-22s | %-18s | %-12s | %-26s\n" "ssle-analytics" "$(incus list ssle-analytics --format csv -c 4 | awk '{print $1}')" "5003" "Math Average Worker"
printf "%-22s | %-18s | %-12s | %-26s\n" "ssle-temperature" "$(incus list ssle-temperature --format csv -c 4 | awk '{print $1}')" "5000" "Edge HMAC Telemetry Sensor"
printf "%-22s | %-18s | %-12s | %-26s\n" "ssle-exporter" "$(incus list ssle-exporter --format csv -c 4 | awk '{print $1}')" "8000, 8001" "Prometheus Exporters"
if [ "$DEPLOY_MONITORING" = true ]; then
    printf "%-22s | %-18s | %-12s | %-26s\n" "prometheus-container" "$PROM_IP" "9090" "Prometheus Server & Alerts"
fi
if [ "$DEPLOY_WAZUH" = true ]; then
    printf "%-22s | %-18s | %-12s | %-26s\n" "wazuh-container" "$WAZUH_IP" "1514, 1515" "Wazuh SIEM Manager"
fi
echo "--------------------------------------------------------------------------------"

echo -e "\n${GREEN}${BOLD}Useful Access Endpoints:${NC}"
echo -e "  - Prometheus Dashboard:     ${CYAN}http://${PROM_IP}:9090${NC}"
echo -e "  - Prometheus Targets:       ${CYAN}http://${PROM_IP}:9090/targets${NC}"
echo -e "  - Service Registry:         ${CYAN}http://${REG_IP}:5050/services${NC}"
echo -e "  - Incident Localization:    ${CYAN}http://${REG_IP}:5050/services/locate/ingestion${NC}"
echo -e "  - Ingestion Gateway:        ${CYAN}http://${INGEST_IP}:5001/health${NC}"
echo -e "  - Ingestion Access Log:     ${DIM}incus exec ssle-ingestion -- tail -f /var/log/ssle/ingestion_access.log${NC}"
echo -e "  - Wazuh Alerts Log:         ${DIM}incus exec wazuh-container -- tail -f /var/ossec/logs/alerts/alerts.log${NC}"
echo -e "\n${YELLOW}To tear down all containers and shared data when done, execute:${NC}"
echo -e "  ${BOLD}./cleanup.sh${NC}\n"
