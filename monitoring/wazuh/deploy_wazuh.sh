#!/usr/bin/env bash
# ==============================================================================
# SSLE Practical Guide 4: Automated Wazuh SIEM Manager Deployment (Incus)
# ==============================================================================
set -euo pipefail

CONTAINER_NAME="wazuh-container"
BASE_IMAGE="images:ubuntu/22.04"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=========================================================="
echo "  Deploying Wazuh SIEM Manager (SSLE Guide 4)"
echo "=========================================================="

# 1. Launch Wazuh container if not already running
if incus info "$CONTAINER_NAME" &>/dev/null; then
    echo "==> Container $CONTAINER_NAME already exists."
else
    echo "==> Launching $CONTAINER_NAME using $BASE_IMAGE..."
    incus launch "$BASE_IMAGE" "$CONTAINER_NAME" \
        -c limits.cpu=2 \
        -c limits.memory=2GiB
    echo "==> Waiting for container initialization (8s)..."
    sleep 8
fi

# 2. Check if Wazuh manager is already installed
echo "==> Checking Wazuh installation inside $CONTAINER_NAME..."
if ! incus exec "$CONTAINER_NAME" -- which wazuh-control &>/dev/null; then
    echo "==> Adding Wazuh repository and installing wazuh-manager..."
    incus exec "$CONTAINER_NAME" -- bash -c '
        apt-get update && apt-get install -y curl apt-transport-https lsb-release gnupg2
        curl -s https://packages.wazuh.com/key/GPG-KEY-WAZUH | gpg --no-default-keyring --keyring gnupg-ring:/usr/share/keyrings/wazuh.gpg --import && chmod 644 /usr/share/keyrings/wazuh.gpg
        echo "deb [signed-by=/usr/share/keyrings/wazuh.gpg] https://packages.wazuh.com/4.x/apt/ stable main" > /etc/apt/sources.list.d/wazuh.list
        apt-get update
        apt-get install -y wazuh-manager python3
    '
fi

# 3. Deploy custom detection rules and decoders
echo "==> Deploying custom rules and decoders to Wazuh Manager..."
incus file push "${SCRIPT_DIR}/rules/local_rules.xml" "$CONTAINER_NAME/var/ossec/etc/rules/local_rules.xml"
incus file push "${SCRIPT_DIR}/decoders/local_decoder.xml" "$CONTAINER_NAME/var/ossec/etc/decoders/local_decoder.xml"

# 4. Deploy AlienVault IP reputation list and build CDB
echo "==> Deploying AlienVault IP reputation list..."
incus exec "$CONTAINER_NAME" -- mkdir -p /var/ossec/etc/lists
incus file push "${SCRIPT_DIR}/lists/alienvault_reputation" "$CONTAINER_NAME/var/ossec/etc/lists/alienvault_reputation"

# Compile CDB list inside container
echo "==> Compiling CDB list with wazuh-makelists..."
incus exec "$CONTAINER_NAME" -- bash -c '
    if [ -x /var/ossec/bin/wazuh-makelists ]; then
        /var/ossec/bin/wazuh-makelists
    elif [ -x /var/ossec/bin/ossec-makelists ]; then
        /var/ossec/bin/ossec-makelists
    fi
'

# 5. Configure Active Response and rules in ossec.conf
echo "==> Applying Active Response configuration..."
incus exec "$CONTAINER_NAME" -- bash -c '
    # Ensure AlienVault list and Active Response are declared in ossec.conf if not already present
    if ! grep -q "alienvault_reputation" /var/ossec/etc/ossec.conf; then
        sed -i "/<\/ruleset>/i \    <list>etc/lists/alienvault_reputation</list>" /var/ossec/etc/ossec.conf
    fi

    if ! grep -q "rules_id>100100" /var/ossec/etc/ossec.conf; then
        cat << "EOF" >> /var/ossec/etc/ossec.conf

  <!-- SSLE Active Response for Shellshock & Threat Intel -->
  <active-response>
    <command>firewall-drop</command>
    <location>local</location>
    <rules_id>100100, 100101</rules_id>
    <timeout>60</timeout>
  </active-response>
EOF
    fi
'

# 6. Restart Wazuh manager
echo "==> Restarting wazuh-manager..."
incus exec "$CONTAINER_NAME" -- systemctl restart wazuh-manager

# 7. Print manager details
CONTAINER_IP=$(incus list "$CONTAINER_NAME" --format csv -c 4 | awk '{print $1}')
echo ""
echo "=========================================================="
echo "  Wazuh SIEM Manager deployed successfully!"
echo "  Container:    $CONTAINER_NAME"
echo "  Manager IP:   $CONTAINER_IP"
echo "  Rules:        Shellshock (100100), AlienVault (100101)"
echo "  Active Resp:  firewall-drop (60s ban)"
echo "=========================================================="
echo "Next step: Install the agent on the target container using:"
echo "  ./monitoring/wazuh/install_agent.sh $CONTAINER_IP ssle-ingestion"
echo "=========================================================="
